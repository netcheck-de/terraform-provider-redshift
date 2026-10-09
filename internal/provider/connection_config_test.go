package provider

import (
	"context"
	"testing"

	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passwordProvider supplies known direct credentials without requiring AWS configuration.
func passwordProvider() providerModel {
	return providerModel{Database: types.StringValue("admin"), Connection: &directConnectionModel{Host: types.StringValue("Warehouse.EXAMPLE.com."), Username: types.StringValue("reader"), Password: types.StringValue("secret")}}
}

// connectionProviderConfig serializes a complete provider model using the actual schema, including nested blocks.
func connectionProviderConfig(t *testing.T, p *redshiftProvider, data providerModel) tfsdk.Config {
	t.Helper()
	var schema framework.SchemaResponse
	p.Schema(context.Background(), framework.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	diagnostics := state.Set(context.Background(), &data)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	return tfsdk.Config(state)
}

// TestConnectionSelectionAndIdentity preserves Serverless IDs while supporting cluster and endpoint bindings.
func TestConnectionSelectionAndIdentity(t *testing.T) {
	for _, mode := range []string{"workgroup", "cluster", "password", "serverless IAM", "cluster IAM", "unknown host", "unknown port", "unknown username", "unknown password", "unknown IAM host", "unknown IAM port", "new warehouse and secret"} {
		data := providerModel{Region: types.StringValue("eu-central-1"), Database: types.StringValue("admin")}
		switch mode {
		case "workgroup":
			data.Workgroup = types.StringValue("warehouse")
		case "new warehouse and secret":
			data.Workgroup, data.SecretARN = types.StringUnknown(), types.StringUnknown()
		case "cluster":
			data.ClusterIdentifier, data.DBUser = types.StringValue("cluster"), types.StringValue("reader")
		case "password", "unknown host", "unknown port", "unknown username", "unknown password":
			data = passwordProvider()
			if mode == "unknown username" {
				data.Connection.Username = types.StringUnknown()
			}
			if mode == "unknown password" {
				data.Connection.Password = types.StringUnknown()
			}
			if mode == "unknown host" {
				data.Connection.Host = types.StringUnknown()
			}
			if mode == "unknown port" {
				data.Connection.Port = types.Int64Unknown()
			}
		case "serverless IAM":
			data.Connection = &directConnectionModel{IAM: &iamConnectionModel{Workgroup: types.StringValue("warehouse")}}
		case "unknown IAM host":
			data.Connection = &directConnectionModel{Host: types.StringUnknown(), IAM: &iamConnectionModel{Workgroup: types.StringValue("warehouse")}}
		case "unknown IAM port":
			data.Connection = &directConnectionModel{Port: types.Int64Unknown(), IAM: &iamConnectionModel{Workgroup: types.StringValue("warehouse")}}
		case "cluster IAM":
			data.Connection = &directConnectionModel{IAM: &iamConnectionModel{ClusterIdentifier: types.StringValue("cluster"), DBUser: types.StringValue("reader")}}
		}
		require.NoError(t, data.validate(data.deferred()), mode)
		binding := data.binding()
		switch mode {
		case "unknown username", "unknown password", "unknown IAM host", "unknown IAM port":
			// The warehouse is known, but SQL must still wait for the deferred credentials or endpoint.
		case "workgroup", "serverless IAM":
			assert.Equal(t, "workgroup_name", binding.field)
			assert.Equal(t, "warehouse", binding.value.ValueString())
		case "cluster", "cluster IAM":
			assert.Equal(t, "cluster_identifier", binding.field)
			assert.Equal(t, "cluster", binding.value.ValueString())
		case "password":
			assert.Equal(t, "endpoint", binding.field)
			assert.Equal(t, "warehouse.example.com:5439", binding.value.ValueString())
			data.Connection.Port = types.Int64Value(5440)
			assert.Equal(t, "warehouse.example.com:5440", data.binding().value.ValueString())
		default:
			assert.True(t, binding.value.IsUnknown())
		}
		p := New("test")().(*redshiftProvider)
		config := connectionProviderConfig(t, p, data)
		var validated framework.ValidateConfigResponse
		p.ValidateConfig(context.Background(), framework.ValidateConfigRequest{Config: config}, &validated)
		require.False(t, validated.Diagnostics.HasError(), "%v", validated.Diagnostics)
		var resp framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: config}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		switch {
		case data.deferred():
			assert.Nil(t, p.client)
			assert.True(t, resp.ResourceData.(providerData).warehouse.value.IsUnknown())
		case data.Connection != nil:
			assert.IsType(t, &redshiftconn.Client{}, p.client)
		default:
			assert.IsType(t, &dataapi.Client{}, p.client)
		}
	}
}

// TestConnectionValidationRejectsAmbiguousAuthentication checks selectors, required credentials, and unknown auth inputs.
func TestConnectionValidationRejectsAmbiguousAuthentication(t *testing.T) {
	for _, mode := range []string{"none", "two selectors", "cluster auth missing", "serverless user", "two credentials", "unknown user", "unknown secret", "empty workgroup", "connection plus auth", "port low", "port high", "unknown CA", "host absent", "username absent", "password empty", "IAM plus password", "IAM selector missing", "IAM selectors both", "serverless IAM user", "cluster IAM user missing", "cluster IAM user unknown", "empty IAM selector", "empty IAM host"} {
		data := passwordProvider()
		switch mode {
		case "none":
			data.Connection = nil
		case "two selectors":
			data.Workgroup = types.StringValue("warehouse")
		case "cluster auth missing":
			data.Connection = nil
			data.ClusterIdentifier = types.StringValue("cluster")
		case "serverless user":
			data.Connection = nil
			data.Workgroup, data.DBUser = types.StringValue("warehouse"), types.StringValue("reader")
		case "two credentials":
			data.Connection = nil
			data.ClusterIdentifier, data.DBUser, data.SecretARN = types.StringValue("cluster"), types.StringValue("reader"), types.StringValue("secret")
		case "unknown user":
			data.Connection = nil
			data.ClusterIdentifier, data.DBUser = types.StringValue("cluster"), types.StringUnknown()
		case "unknown secret":
			data.Connection = nil
			data.Workgroup, data.SecretARN = types.StringValue("warehouse"), types.StringUnknown()
		case "empty workgroup":
			data.Connection = nil
			data.Workgroup = types.StringValue("")
		case "connection plus auth":
			data.SecretARN = types.StringValue("secret")
		case "port low":
			data.Connection.Port = types.Int64Value(0)
		case "port high":
			data.Connection.Port = types.Int64Value(65536)
		case "unknown CA":
			data.Connection.CACertFile = types.StringUnknown()
		case "host absent":
			data.Connection.Host = types.StringNull()
		case "username absent":
			data.Connection.Username = types.StringNull()
		case "password empty":
			data.Connection.Password = types.StringValue("")
		case "IAM plus password":
			data.Connection.IAM = &iamConnectionModel{Workgroup: types.StringValue("warehouse")}
		default:
			data.Connection = &directConnectionModel{IAM: &iamConnectionModel{}}
			if mode == "IAM selectors both" {
				data.Connection.IAM.Workgroup, data.Connection.IAM.ClusterIdentifier = types.StringValue("warehouse"), types.StringValue("cluster")
			}
			if mode == "serverless IAM user" {
				data.Connection.IAM.Workgroup, data.Connection.IAM.DBUser = types.StringValue("warehouse"), types.StringValue("reader")
			}
			if mode == "cluster IAM user missing" {
				data.Connection.IAM.ClusterIdentifier = types.StringValue("cluster")
			}
			if mode == "cluster IAM user unknown" {
				data.Connection.IAM.ClusterIdentifier, data.Connection.IAM.DBUser = types.StringValue("cluster"), types.StringUnknown()
			}
			if mode == "empty IAM selector" {
				data.Connection.IAM.Workgroup = types.StringValue("")
			}
			if mode == "empty IAM host" {
				data.Connection.IAM.Workgroup, data.Connection.Host = types.StringValue("warehouse"), types.StringValue("")
			}
		}
		require.Error(t, data.validate(false), mode)
		p := New("test")().(*redshiftProvider)
		config := connectionProviderConfig(t, p, data)
		var validated framework.ValidateConfigResponse
		p.ValidateConfig(context.Background(), framework.ValidateConfigRequest{Config: config}, &validated)
		deferred := mode == "unknown user" || mode == "unknown secret" || mode == "unknown CA" || mode == "cluster IAM user unknown"
		assert.Equal(t, !deferred, validated.Diagnostics.HasError(), mode)
		var configured framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: config}, &configured)
		assert.Equal(t, mode != "unknown user" && mode != "unknown secret" && mode != "cluster IAM user unknown", configured.Diagnostics.HasError(), mode)
	}
}

// TestDirectConfigurationDoesNotLoadAWSAndReconfiguresRouting ensures password clients ignore broken AWS profiles.
func TestDirectConfigurationDoesNotLoadAWSAndReconfiguresRouting(t *testing.T) {
	t.Setenv("AWS_PROFILE", "profile-that-does-not-exist")
	p := New("test")().(*redshiftProvider)
	data := passwordProvider()
	var resp framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: connectionProviderConfig(t, p, data)}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	data.Connection.Host = types.StringValue("another.example.com")
	p.Configure(context.Background(), framework.ConfigureRequest{Config: connectionProviderConfig(t, p, data)}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.Equal(t, "another.example.com", p.client.(*redshiftconn.Client).Credentials.Host)
	data.Connection.Port = types.Int64Value(5440)
	p.Configure(context.Background(), framework.ConfigureRequest{Config: connectionProviderConfig(t, p, data)}, &resp)
	require.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, uint16(5440), p.client.(*redshiftconn.Client).Credentials.Port)
	config := connectionProviderConfig(t, p, data)
	config.Raw = tftypes.NewValue(tftypes.String, "invalid")
	var validated framework.ValidateConfigResponse
	p.ValidateConfig(context.Background(), framework.ValidateConfigRequest{Config: config}, &validated)
	require.True(t, validated.Diagnostics.HasError())
}
