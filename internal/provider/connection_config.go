package provider

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
)

// directConnectionModel configures a TLS PostgreSQL-wire connection with password or IAM authentication.
type directConnectionModel struct {
	// Host is the endpoint override or the required host for password authentication.
	Host types.String `tfsdk:"host"`
	// Port overrides the discovered/default port; null means 5439 for password connections.
	Port types.Int64 `tfsdk:"port"`
	// Username is the existing SQL user for password authentication.
	Username types.String `tfsdk:"username"`
	// Password is sensitive provider configuration and may be supplied by an ephemeral variable.
	Password types.String `tfsdk:"password"`
	// CACertFile optionally augments system trust with a PEM CA certificate bundle.
	CACertFile types.String `tfsdk:"ca_cert_file"`
	// SSLMode selects TLS verification; null means verify-full.
	SSLMode types.String `tfsdk:"sslmode"`
	// ConnectTimeout bounds connection establishment as a duration string; null means 30s.
	ConnectTimeout types.String `tfsdk:"connect_timeout"`
	// IAM selects an AWS warehouse whose temporary credentials authenticate each new connection.
	IAM *iamConnectionModel `tfsdk:"iam"`
}

// iamConnectionModel selects exactly one AWS warehouse for endpoint discovery and temporary SQL credentials.
type iamConnectionModel struct {
	// Workgroup is a Serverless name or ARN, matching the Data API identity spelling.
	Workgroup types.String `tfsdk:"workgroup_name"`
	// ClusterIdentifier is a provisioned cluster name.
	ClusterIdentifier types.String `tfsdk:"cluster_identifier"`
	// DBUser selects an existing SQL user for provisioned cluster credentials.
	DBUser types.String `tfsdk:"db_user"`
}

// connectionSchema defines the direct_connection block without requiring connection establishment.
func connectionSchema() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		MarkdownDescription: "Direct TLS SQL connection. Use username/password or an IAM warehouse selector, never both.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true, MarkdownDescription: "Endpoint hostname; required for password authentication, optional override for IAM."},
			"port":         schema.Int64Attribute{Optional: true, MarkdownDescription: "Port 1–65535; defaults to 5439 or the IAM-discovered endpoint port."},
			"username":     schema.StringAttribute{Optional: true, MarkdownDescription: "Existing SQL user for password authentication."},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "SQL password; supports ephemeral input and is never part of resource state."},
			"ca_cert_file": schema.StringAttribute{Optional: true, MarkdownDescription: "PEM CA bundle added to system trust for `verify-full` and `verify-ca`."},
			"sslmode": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "TLS mode: `verify-full` (default) verifies the certificate chain and hostname, `verify-ca` only the chain, " +
					"`require` encrypts without verifying the server, and `disable` connects without TLS. Anything weaker than `verify-full` " +
					"exposes credentials to impersonation or eavesdropping.",
				Validators: []validator.String{stringvalidator.OneOf(redshiftconn.SSLModes...)},
			},
			"connect_timeout": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Maximum time to open one connection, covering TCP connect, TLS handshake, and authentication, as a duration " +
					"such as `10s`; defaults to `30s`. IAM credential lookups are bounded by `query_timeout` instead. " +
					"`PGCONNECT_TIMEOUT` and other `PG*` environment variables do not override it.",
				Validators: []validator.String{durationValidator{}},
			},
		},
		Blocks: map[string]schema.Block{
			"iam": schema.SingleNestedBlock{MarkdownDescription: "Obtain temporary SQL credentials for one Serverless workgroup or provisioned cluster.", Attributes: map[string]schema.Attribute{
				"workgroup_name":     schema.StringAttribute{Optional: true, MarkdownDescription: "Serverless workgroup name or ARN."},
				"cluster_identifier": schema.StringAttribute{Optional: true, MarkdownDescription: "Provisioned cluster identifier."},
				"db_user":            schema.StringAttribute{Optional: true, MarkdownDescription: "Existing SQL user; required for clusters and prohibited for Serverless."},
			}},
		},
	}
}

// queryTimeoutEnv supplies query_timeout when the argument is omitted.
const queryTimeoutEnv = "REDSHIFT_QUERY_TIMEOUT"

// Defaults and limits of the transport-wide settings; the default timeout keeps the behavior of earlier releases.
const (
	// defaultQueryTimeout bounds one SQL statement.
	defaultQueryTimeout = "5m"
	// applicationNamePrefix precedes the provider version in the default application_name.
	applicationNamePrefix = "terraform-provider-redshift/"
	// maxRetries caps max_retries; with the SDK's 20s backoff ceiling it already allows more than half an hour of
	// retries per call, and a bound keeps the attempt count from overflowing into the SDK's "unlimited".
	maxRetries = 100
	// maxApplicationName is the width of the application_name column in Redshift connection logs, well below the
	// Data API StatementName limit.
	maxApplicationName = 250
)

// retryModes lists the AWS SDK retry modes accepted by retry_mode.
var retryModes = []string{string(aws.RetryModeStandard), string(aws.RetryModeAdaptive)}

// applicationNamePattern restricts application_name to printable ASCII, which both transports pass through verbatim.
var applicationNamePattern = regexp.MustCompile(`^[\x20-\x7e]+$`)

// transportSettings holds the resolved limits and labels shared by both SQL transports.
type transportSettings struct {
	// queryTimeout bounds one statement, including credential lookup, connection, execution, and result retrieval.
	queryTimeout time.Duration
	// connectTimeout bounds connection establishment for direct connections.
	connectTimeout time.Duration
	// applicationName labels sessions and Data API statements.
	applicationName string
}

// durationValidator accepts positive time.ParseDuration strings.
type durationValidator struct{}

// Description explains the accepted duration format.
func (durationValidator) Description(context.Context) string {
	return "value must be a positive duration such as 30s, 5m, or 1h30m"
}

// MarkdownDescription explains the accepted duration format.
func (v durationValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

// ValidateString rejects malformed or non-positive durations while deferring unknown values to configuration.
func (durationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := parseDuration(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid duration", err.Error())
	}
}

// parseDuration parses a positive duration; zero would disable a timeout rather than tighten it.
func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use a number with a unit such as 30s, 5m, or 1h", value)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", value)
	}
	return duration, nil
}

// validateTransport checks the transport-wide settings. They shape every statement and AWS call, so unlike
// credentials they cannot be deferred until apply.
func (data providerModel) validateTransport(allowUnknown bool) error {
	durations := map[string]types.String{"query_timeout": data.QueryTimeout}
	if data.Connection != nil {
		durations["direct_connection.connect_timeout"] = data.Connection.ConnectTimeout
	}
	unknown := data.MaxRetries.IsUnknown() || data.RetryMode.IsUnknown() || data.ApplicationName.IsUnknown()
	for _, value := range durations {
		unknown = unknown || value.IsUnknown()
	}
	if !allowUnknown && unknown {
		return fmt.Errorf("query_timeout, max_retries, retry_mode, application_name, and direct_connection.connect_timeout must be known during configuration")
	}
	for name, value := range durations {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		if _, err := parseDuration(value.ValueString()); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if retries := data.MaxRetries; !retries.IsNull() && !retries.IsUnknown() && (retries.ValueInt64() < 0 || retries.ValueInt64() > maxRetries) {
		return fmt.Errorf("max_retries must be between 0 and %d", maxRetries)
	}
	if mode := data.RetryMode; !mode.IsNull() && !mode.IsUnknown() && !slices.Contains(retryModes, mode.ValueString()) {
		return fmt.Errorf("retry_mode must be one of %s", strings.Join(retryModes, ", "))
	}
	if name := data.ApplicationName; !name.IsNull() && !name.IsUnknown() {
		if len(name.ValueString()) > maxApplicationName || !applicationNamePattern.MatchString(name.ValueString()) {
			return fmt.Errorf("application_name must be 1 to %d printable ASCII characters", maxApplicationName)
		}
	}
	return nil
}

// transport resolves the transport-wide settings from known configuration, the environment, and defaults.
func (data providerModel) transport(version string) (transportSettings, error) {
	settings := transportSettings{connectTimeout: redshiftconn.DefaultConnectTimeout, applicationName: applicationNamePrefix + version}
	timeout, source := data.QueryTimeout.ValueString(), "query_timeout"
	if data.QueryTimeout.IsNull() {
		timeout = defaultQueryTimeout
		if value := os.Getenv(queryTimeoutEnv); value != "" {
			timeout, source = value, queryTimeoutEnv
		}
	}
	var err error
	if settings.queryTimeout, err = parseDuration(timeout); err != nil {
		return transportSettings{}, fmt.Errorf("%s: %w", source, err)
	}
	if data.Connection != nil && !data.Connection.ConnectTimeout.IsNull() {
		if settings.connectTimeout, err = parseDuration(data.Connection.ConnectTimeout.ValueString()); err != nil {
			return transportSettings{}, fmt.Errorf("direct_connection.connect_timeout: %w", err)
		}
	}
	if !data.ApplicationName.IsNull() {
		settings.applicationName = data.ApplicationName.ValueString()
	}
	return settings, nil
}

// validate checks selector/authentication combinations while allowing unknown warehouse and endpoint values.
func (data providerModel) validate(allowUnknown bool) error {
	selectors := 0
	if !data.Workgroup.IsNull() {
		selectors++
	}
	if !data.ClusterIdentifier.IsNull() {
		selectors++
	}
	if data.Connection != nil {
		selectors++
	}
	if selectors != 1 {
		return fmt.Errorf("configure exactly one of workgroup_name, cluster_identifier, or direct_connection")
	}
	if err := data.validateTransport(allowUnknown); err != nil {
		return err
	}
	if !allowUnknown && (data.Region.IsUnknown() || data.Profile.IsUnknown()) {
		return fmt.Errorf("region and profile must be known during configuration")
	}
	if data.Connection == nil {
		if !data.Workgroup.IsNull() && !data.DBUser.IsNull() {
			return fmt.Errorf("db_user is supported only for provisioned Data API connections")
		}
		if !data.DBUser.IsNull() && !data.SecretARN.IsNull() {
			return fmt.Errorf("db_user and secret_arn are mutually exclusive")
		}
		if !data.ClusterIdentifier.IsNull() && data.DBUser.IsNull() && data.SecretARN.IsNull() {
			return fmt.Errorf("provisioned Data API connections require db_user or secret_arn")
		}
		if !allowUnknown && (data.DBUser.IsUnknown() || data.SecretARN.IsUnknown()) {
			return fmt.Errorf("data API authentication arguments must be known during configuration")
		}
		for name, value := range map[string]types.String{"workgroup_name": data.Workgroup, "cluster_identifier": data.ClusterIdentifier, "db_user": data.DBUser, "secret_arn": data.SecretARN} {
			if !value.IsNull() && !value.IsUnknown() && value.ValueString() == "" {
				return fmt.Errorf("%s must not be empty", name)
			}
		}
		return nil
	}
	if !data.DBUser.IsNull() || !data.SecretARN.IsNull() {
		return fmt.Errorf("top-level db_user and secret_arn cannot be used with direct_connection")
	}
	connection := data.Connection
	if !connection.Port.IsNull() && !connection.Port.IsUnknown() && (connection.Port.ValueInt64() < 1 || connection.Port.ValueInt64() > 65535) {
		return fmt.Errorf("direct_connection.port must be between 1 and 65535")
	}
	if !allowUnknown && (connection.CACertFile.IsUnknown() || connection.SSLMode.IsUnknown()) {
		return fmt.Errorf("direct_connection.ca_cert_file and sslmode must be known during configuration")
	}
	if mode := connection.SSLMode.ValueString(); !connection.CACertFile.IsNull() && (mode == redshiftconn.SSLModeRequire || mode == redshiftconn.SSLModeDisable) {
		return fmt.Errorf("direct_connection.ca_cert_file has no effect with sslmode %q", mode)
	}
	if connection.IAM == nil {
		for name, value := range map[string]types.String{"host": connection.Host, "username": connection.Username, "password": connection.Password} {
			if value.IsNull() || (!value.IsUnknown() && value.ValueString() == "") {
				return fmt.Errorf("password connections require nonempty direct_connection.%s", name)
			}
		}
		return nil
	}
	if !connection.Username.IsNull() || !connection.Password.IsNull() {
		return fmt.Errorf("direct_connection.iam conflicts with username and password")
	}
	iam := connection.IAM
	if iam.Workgroup.IsNull() == iam.ClusterIdentifier.IsNull() {
		return fmt.Errorf("direct_connection.iam requires exactly one of workgroup_name or cluster_identifier")
	}
	if !iam.Workgroup.IsNull() && !iam.DBUser.IsNull() {
		return fmt.Errorf("serverless IAM credentials determine the SQL user; omit db_user")
	}
	if !iam.ClusterIdentifier.IsNull() && (iam.DBUser.IsNull() || (!iam.DBUser.IsUnknown() && iam.DBUser.ValueString() == "")) {
		return fmt.Errorf("cluster IAM authentication requires an existing db_user")
	}
	if !allowUnknown && iam.DBUser.IsUnknown() {
		return fmt.Errorf("direct_connection.iam.db_user must be known during configuration")
	}
	for name, value := range map[string]types.String{"workgroup_name": iam.Workgroup, "cluster_identifier": iam.ClusterIdentifier, "host": connection.Host} {
		if !value.IsNull() && !value.IsUnknown() && value.ValueString() == "" {
			return fmt.Errorf("direct_connection IAM %s must not be empty", name)
		}
	}
	return nil
}

// deferred reports whether credentials or routing are unknown, so no SQL may run until apply reconfigures the provider.
// Building a client from unknown values would silently substitute empty credentials or a discovered endpoint.
func (data providerModel) deferred() bool {
	if data.DBUser.IsUnknown() || data.SecretARN.IsUnknown() {
		return true
	}
	connection := data.Connection
	if connection == nil {
		return false
	}
	if connection.Host.IsUnknown() || connection.Port.IsUnknown() || connection.Username.IsUnknown() || connection.Password.IsUnknown() {
		return true
	}
	return connection.IAM != nil && connection.IAM.DBUser.IsUnknown()
}

// binding identifies the warehouse independently of transport while retaining legacy Serverless IDs verbatim.
func (data providerModel) binding() warehouseBinding {
	workgroup, cluster := data.Workgroup, data.ClusterIdentifier
	if data.Connection != nil {
		if data.Connection.IAM != nil {
			workgroup, cluster = data.Connection.IAM.Workgroup, data.Connection.IAM.ClusterIdentifier
		} else {
			connection := data.Connection
			if connection.Host.IsUnknown() || connection.Port.IsUnknown() {
				return warehouseBinding{field: "endpoint", value: types.StringUnknown()}
			}
			port := int64(5439)
			if !connection.Port.IsNull() {
				port = connection.Port.ValueInt64()
			}
			host := strings.ToLower(strings.TrimSuffix(connection.Host.ValueString(), "."))
			return warehouseBinding{field: "endpoint", value: types.StringValue(net.JoinHostPort(host, strconv.FormatInt(port, 10)))}
		}
	}
	if !workgroup.IsNull() {
		return warehouseBinding{field: "workgroup_name", value: workgroup}
	}
	return warehouseBinding{field: "cluster_identifier", value: cluster}
}
