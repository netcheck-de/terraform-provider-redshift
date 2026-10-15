// Package provider implements Terraform lifecycles for Redshift SQL objects.
package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// redshiftProvider configures one warehouse's SQL transport and publishes Terraform factories.
type redshiftProvider struct {
	// version is the build version reported to Terraform.
	version string
	// client is the configured SQL transport, or an injected test implementation.
	client sqlclient.Client
	// datashareARN optionally injects read-only datashare discovery for offline tests.
	datashareARN func(context.Context, shareSource) (string, error)
}

// providerData is the shared configuration payload passed to resources and data sources.
type providerData struct {
	// client executes SQL without exposing its transport to lifecycle code.
	client sqlclient.Client
	// warehouse identifies ownership independently of query routing.
	warehouse warehouseBinding
	// database is the local administration database used for catalog reads.
	database types.String
	// datashareARN resolves the producer ARN without assuming the consumer's region.
	datashareARN func(context.Context, shareSource) (string, error)
}

// warehouseBinding identifies SQL ownership independently of the execution transport.
type warehouseBinding struct {
	// field is the JSON import key representing this warehouse kind.
	field string
	// value is its configured identity, which can be unknown during planning.
	value types.String
}

// providerModel contains mutually exclusive Data API/direct selectors and their authentication settings.
type providerModel struct {
	// Region overrides AWS SDK region resolution when supplied.
	Region types.String `tfsdk:"region"`
	// Profile selects an AWS shared configuration profile for this alias.
	Profile types.String `tfsdk:"profile"`
	// Workgroup identifies the warehouse and may be unknown until apply.
	Workgroup types.String `tfsdk:"workgroup_name"`
	// Database selects the existing local administration database.
	Database types.String `tfsdk:"database"`
	// ClusterIdentifier selects a provisioned Data API warehouse.
	ClusterIdentifier types.String `tfsdk:"cluster_identifier"`
	// DBUser selects an existing SQL user for provisioned Data API credentials.
	DBUser types.String `tfsdk:"db_user"`
	// SecretARN selects Data API Secrets Manager authentication instead of temporary credentials.
	SecretARN types.String `tfsdk:"secret_arn"`
	// Connection selects the direct SQL transport instead of either Data API selector.
	Connection *directConnectionModel `tfsdk:"direct_connection"`
	// QueryTimeout bounds one SQL statement as a duration string; null falls back to REDSHIFT_QUERY_TIMEOUT, then 5m.
	QueryTimeout types.String `tfsdk:"query_timeout"`
	// MaxRetries counts AWS API retries after the first attempt; null keeps the AWS SDK configuration.
	MaxRetries types.Int64 `tfsdk:"max_retries"`
	// RetryMode selects the AWS SDK retry strategy; null keeps the AWS SDK configuration.
	RetryMode types.String `tfsdk:"retry_mode"`
	// ApplicationName labels SQL sessions and Data API statements; null means terraform-provider-redshift/<version>.
	ApplicationName types.String `tfsdk:"application_name"`
}

// New returns a provider factory carrying the supplied build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &redshiftProvider{version: version} }
}

// Metadata reports the provider type and build version to Terraform.
func (p *redshiftProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "redshift"
	resp.Version = p.version
}

// Schema defines AWS credential selection and the administration warehouse/database binding.
func (p *redshiftProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Redshift SQL objects through the Data API or a direct TLS SQL connection.",
		Attributes: map[string]schema.Attribute{
			"region":             schema.StringAttribute{Optional: true, MarkdownDescription: "AWS Region; defaults to the AWS SDK configuration, such as the `AWS_REGION` environment variable."},
			"profile":            schema.StringAttribute{Optional: true, MarkdownDescription: "AWS shared configuration profile; defaults to the `AWS_PROFILE` environment variable or the AWS SDK default credential chain."},
			"workgroup_name":     schema.StringAttribute{Optional: true, MarkdownDescription: "Serverless Data API workgroup name or ARN; conflicts with cluster_identifier and direct_connection."},
			"cluster_identifier": schema.StringAttribute{Optional: true, MarkdownDescription: "Provisioned Data API cluster identifier; conflicts with workgroup_name and direct_connection."},
			"db_user":            schema.StringAttribute{Optional: true, MarkdownDescription: "Existing SQL user for cluster Data API authentication; conflicts with secret_arn."},
			"secret_arn":         schema.StringAttribute{Optional: true, MarkdownDescription: "Data API Secrets Manager credentials; conflicts with db_user and direct_connection."},
			"database":           schema.StringAttribute{Required: true, MarkdownDescription: "Existing local administration database for catalog queries."},
			"query_timeout": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Maximum time for one SQL statement, including IAM credential lookup, connection, execution, and result " +
					"retrieval, as a duration such as `90s` or `1h`. Defaults to the `" + queryTimeoutEnv + "` environment variable, " +
					"then `" + defaultQueryTimeout + "`. An earlier deadline of the running Terraform operation still applies.",
				Validators: []validator.String{durationValidator{}},
			},
			"max_retries": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Maximum number of times an AWS API call is retried when AWS throttles requests or you experience transient " +
					"failures, so a call makes at most `max_retries + 1` attempts. Applies to Data API, endpoint discovery, and IAM " +
					"credential calls, not to SQL sent over `direct_connection`. If omitted, the AWS SDK configuration applies: the " +
					"`AWS_MAX_ATTEMPTS` environment variable or the shared configuration parameter `max_attempts` (both count attempts, " +
					"not retries), otherwise 3 attempts. At most " + strconv.Itoa(maxRetries) + ".",
				Validators: []validator.Int64{int64validator.Between(0, maxRetries)},
			},
			"retry_mode": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Specifies how AWS API retries are attempted. Valid values are `standard` and `adaptive`. If omitted, the " +
					"`AWS_RETRY_MODE` environment variable or the shared configuration parameter `retry_mode` applies, otherwise `standard`.",
				Validators: []validator.String{stringvalidator.OneOf(retryModes...)},
			},
			"application_name": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Name reported as the `application_name` of direct SQL sessions and as the Data API statement name, " +
					"as shown in `SYS_CONNECTION_LOG` and `ListStatements`. Up to 250 printable ASCII characters; defaults to " +
					"`" + applicationNamePrefix + "<version>`.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, maxApplicationName),
					stringvalidator.RegexMatches(applicationNamePattern, "must contain only printable ASCII characters"),
				},
			},
		},
		Blocks: map[string]schema.Block{"direct_connection": connectionSchema()},
	}
}

// Configure constructs the SQL transport without issuing SQL and distributes its ownership binding.
func (p *redshiftProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deferred := data.deferred()
	if err := data.validate(deferred); err != nil {
		resp.Diagnostics.AddError("Invalid connection configuration", err.Error())
		return
	}
	if deferred {
		// Credentials or endpoints can be created alongside their warehouse.
		// Retain an unknown binding so no SQL can run with fallback credentials;
		// Terraform reconfigures the provider with known credentials during apply.
		p.client = nil
		binding := data.binding()
		binding.value = types.StringUnknown()
		resp.ResourceData = providerData{warehouse: binding, database: data.Database}
		resp.DataSourceData = resp.ResourceData
		return
	}
	// Reconfiguration must not retain credentials or routing from an earlier configuration.
	switch p.client.(type) {
	case *dataapi.Client, *redshiftconn.Client:
		p.client = nil
	}
	if p.client == nil {
		client, err := data.sqlClient(ctx, p.version)
		if err != nil {
			resp.Diagnostics.AddError("Configure SQL connection", err.Error())
			return
		}
		p.client = client
	}
	lookup := p.datashareARN
	if lookup == nil {
		lookup = data.datashareARN
	}
	resp.ResourceData = providerData{client: sqlclient.SerializeMutations(p.client), warehouse: data.binding(), database: data.Database, datashareARN: lookup}
	resp.DataSourceData = resp.ResourceData
}

// ValidateConfig rejects incompatible selectors and authentication modes before Terraform plans SQL objects.
func (p *redshiftProvider) ValidateConfig(ctx context.Context, req provider.ValidateConfigRequest, resp *provider.ValidateConfigResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := data.validate(true); err != nil {
		resp.Diagnostics.AddError("Invalid connection configuration", err.Error())
	}
}

// Resources returns the registered SQL object and permission lifecycle factories in type-name order.
func (p *redshiftProvider) Resources(context.Context) []func() resource.Resource {
	return registeredResources()
}

// DataSources returns the registered read-only catalog lookup factories in type-name order.
func (p *redshiftProvider) DataSources(context.Context) []func() datasource.DataSource {
	return registeredDataSources()
}
