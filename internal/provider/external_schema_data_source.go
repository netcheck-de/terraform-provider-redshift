package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalSchemaDataSource exposes an existing external schema mapping.
type externalSchemaDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// externalSchemaData contains mapping lookup keys and the observed source form, options, and owner.
type externalSchemaData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Database contains the external schema mapping.
	Database types.String `tfsdk:"database"`
	// Name identifies the external SQL schema.
	Name types.String `tfsdk:"name"`
	// SourceType reports the CREATE EXTERNAL SCHEMA ... FROM form.
	SourceType types.String `tfsdk:"source_type"`
	// GlueDatabase reports the mapped Glue database of a DATA_CATALOG schema.
	GlueDatabase types.String `tfsdk:"glue_database"`
	// SourceDatabase reports the external database of the other forms.
	SourceDatabase types.String `tfsdk:"source_database"`
	// SourceSchema reports the PostgreSQL or Redshift source schema, if recorded.
	SourceSchema types.String `tfsdk:"source_schema"`
	// IAMRoleARN reports the role used by the mapping, if recorded.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Region reports the Glue catalog or stream region recorded in the mapping.
	Region types.String `tfsdk:"region"`
	// URI reports the source endpoint, if recorded.
	URI types.String `tfsdk:"uri"`
	// Port reports the source port, if recorded.
	Port types.Int64 `tfsdk:"port"`
	// SecretARN reports the credentials or certificate secret, if recorded.
	SecretARN types.String `tfsdk:"secret_arn"`
	// Authentication reports the streaming authentication mode, if recorded.
	Authentication types.String `tfsdk:"authentication"`
	// AuthenticationARN reports the mTLS certificate, if recorded.
	AuthenticationARN types.String `tfsdk:"authentication_arn"`
	// Owner reports the SQL user owning the schema.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerDataSource(newExternalSchemaDataSource)

// externalSchemaOutputDescriptions describe the observed attributes for the lookup and the listing, where the
// resource's text describes configuration.
var externalSchemaOutputDescriptions = map[string]string{
	"source_type":        "`DATA_CATALOG`, `HIVE_METASTORE`, `POSTGRES`, `MYSQL`, `REDSHIFT`, `KINESIS`, or `MSK`.",
	"glue_database":      "Glue database name of a `DATA_CATALOG` schema.",
	"source_database":    "External database of a Hive, federated, or Redshift schema.",
	"source_schema":      "PostgreSQL or Redshift source schema. Null when the catalog does not record it.",
	"iam_role_arn":       "IAM role, or role chain, used to reach the source. Null when the catalog does not record it.",
	"region":             "Glue catalog or stream AWS region. Null when the catalog does not record it.",
	"uri":                "Hive metastore URI, federated hostname, or Kafka bootstrap URI. Null when the catalog does not record it.",
	"port":               "Hive metastore or federated database port. Null when the catalog does not record it.",
	"secret_arn":         "Secrets Manager ARN of the federated credentials or mTLS certificate. Null when the catalog does not record it.",
	"authentication":     "Streaming authentication mode `NONE`, `IAM`, or `MTLS`. Null when the catalog does not record it.",
	"authentication_arn": "ACM certificate ARN used for mTLS. Null when the catalog does not record it.",
	"owner":              "SQL user owning the external schema.",
}

// newExternalSchemaDataSource constructs a read-only external schema lookup.
func newExternalSchemaDataSource() datasource.DataSource { return &externalSchemaDataSource{} }

// Metadata identifies the external schema data source to Terraform.
func (d *externalSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_schema"
}

// Schema defines the external schema lookup and its observed source and owner.
func (d *externalSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id":       dataSourceIDAttribute(),
		"database": schema.StringAttribute{Required: true, MarkdownDescription: "Local Redshift database."},
		"name":     schema.StringAttribute{Required: true, MarkdownDescription: "External schema name."},
	}

	for name, description := range externalSchemaOutputDescriptions {
		if name == "port" {
			attributes[name] = schema.Int64Attribute{Computed: true, MarkdownDescription: description}
		} else {
			attributes[name] = schema.StringAttribute{Computed: true, MarkdownDescription: description}
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an external schema with its source form, recorded options, and owner.",
		Attributes:          attributes,
	}
}

// Read resolves an existing external schema without taking ownership.
func (d *externalSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data externalSchemaData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	current := externalSchemaModel{Database: data.Database, Name: data.Name}

	found, err := (&externalSchemaResource{d.resourceClient}).read(ctx, &current)
	if err != nil {
		resp.Diagnostics.AddError("Read external schema", err.Error())
		return
	}

	if !found {
		resp.Diagnostics.AddError("External schema not found", "No external schema named "+data.Name.ValueString()+" exists in the configured database.")
		return
	}

	data = externalSchemaData{
		Database: data.Database, Name: current.Name, SourceType: current.SourceType, GlueDatabase: current.GlueDatabase,
		SourceDatabase: current.SourceDatabase, SourceSchema: current.SourceSchema, IAMRoleARN: current.IAMRoleARN, Region: current.Region,
		URI: current.URI, Port: current.Port, SecretARN: current.SecretARN, Authentication: current.Authentication,
		AuthenticationARN: current.AuthenticationARN, Owner: current.Owner,
	}
	data.ID = identity(map[string]string{"database": data.Database.ValueString(), "name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
