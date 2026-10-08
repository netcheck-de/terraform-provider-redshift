package provider

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
			"port":         schema.Int64Attribute{Optional: true, MarkdownDescription: "Port 1–65535; default 5439 or the IAM-discovered endpoint port."},
			"username":     schema.StringAttribute{Optional: true, MarkdownDescription: "Existing SQL user for password authentication."},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "SQL password; supports ephemeral input and is never part of resource state."},
			"ca_cert_file": schema.StringAttribute{Optional: true, MarkdownDescription: "PEM CA bundle added to system trust. TLS certificate and hostname verification remain mandatory."},
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
	if !allowUnknown && connection.CACertFile.IsUnknown() {
		return fmt.Errorf("direct_connection.ca_cert_file must be known during configuration")
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
