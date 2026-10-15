package provider

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	"github.com/hashicorp/terraform-plugin-framework/path"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

// TestDirectSSLModeConfiguration passes sslmode to the direct client and rejects CA bundles that would be ignored.
func TestDirectSSLModeConfiguration(t *testing.T) {
	data := passwordProvider()
	data.Connection.SSLMode = types.StringValue(redshiftconn.SSLModeRequire)
	require.NoError(t, data.validate(false))
	client, err := data.sqlClient(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, redshiftconn.SSLModeRequire, client.(*redshiftconn.Client).SSLMode)
	data.Connection.CACertFile = types.StringValue("ca.pem")
	require.ErrorContains(t, data.validate(false), "no effect")
	data.Connection.SSLMode = types.StringValue(redshiftconn.SSLModeVerifyCA)
	require.NoError(t, data.validate(false))
	data.Connection.SSLMode = types.StringUnknown()
	require.ErrorContains(t, data.validate(false), "sslmode must be known")
}

// TestTransportSettingsDefaultsAndOverrides resolves timeouts and the application name from configuration, the
// environment, and defaults for both transports.
func TestTransportSettingsDefaultsAndOverrides(t *testing.T) {
	t.Setenv(queryTimeoutEnv, "")
	data := passwordProvider()
	client, err := data.sqlClient(context.Background(), "1.2.3")
	require.NoError(t, err)
	direct := client.(*redshiftconn.Client)
	assert.Equal(t, 5*time.Minute, direct.Timeout)
	assert.Equal(t, 30*time.Second, direct.ConnectTimeout)
	assert.Equal(t, "terraform-provider-redshift/1.2.3", direct.ApplicationName)

	t.Setenv(queryTimeoutEnv, "90s")
	data.Connection.ConnectTimeout, data.ApplicationName = types.StringValue("10s"), types.StringValue("deploy pipeline")
	client, err = data.sqlClient(context.Background(), "1.2.3")
	require.NoError(t, err)
	direct = client.(*redshiftconn.Client)
	assert.Equal(t, 90*time.Second, direct.Timeout)
	assert.Equal(t, 10*time.Second, direct.ConnectTimeout)
	assert.Equal(t, "deploy pipeline", direct.ApplicationName)

	data.QueryTimeout = types.StringValue("2h")
	client, err = data.sqlClient(context.Background(), "1.2.3")
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, client.(*redshiftconn.Client).Timeout)

	data.QueryTimeout = types.StringNull()
	t.Setenv(queryTimeoutEnv, "soon")
	_, err = data.sqlClient(context.Background(), "1.2.3")
	require.ErrorContains(t, err, queryTimeoutEnv)

	t.Setenv(queryTimeoutEnv, "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	p := New("1.2.3")().(*redshiftProvider)
	workgroup := providerModel{Region: types.StringValue("eu-central-1"), Database: types.StringValue("admin"), Workgroup: types.StringValue("warehouse"), QueryTimeout: types.StringValue("45s")}
	var resp framework.ConfigureResponse
	p.Configure(context.Background(), framework.ConfigureRequest{Config: connectionProviderConfig(t, p, workgroup)}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	dataAPI := p.client.(*dataapi.Client)
	assert.Equal(t, 45*time.Second, dataAPI.Timeout)
	assert.Equal(t, "terraform-provider-redshift/1.2.3", dataAPI.StatementName)
}

// TestTransportSettingsValidation rejects malformed, out-of-range, and unknown transport settings.
func TestTransportSettingsValidation(t *testing.T) {
	for name, change := range map[string]func(*providerModel){
		"query_timeout without unit": func(data *providerModel) { data.QueryTimeout = types.StringValue("300") },
		"query_timeout zero":         func(data *providerModel) { data.QueryTimeout = types.StringValue("0s") },
		"query_timeout negative":     func(data *providerModel) { data.QueryTimeout = types.StringValue("-1m") },
		"connect_timeout malformed":  func(data *providerModel) { data.Connection.ConnectTimeout = types.StringValue("soon") },
		"max_retries negative":       func(data *providerModel) { data.MaxRetries = types.Int64Value(-1) },
		"max_retries above cap":      func(data *providerModel) { data.MaxRetries = types.Int64Value(maxRetries + 1) },
		"max_retries overflowing":    func(data *providerModel) { data.MaxRetries = types.Int64Value(math.MaxInt64) },
		"retry_mode unsupported":     func(data *providerModel) { data.RetryMode = types.StringValue("legacy") },
		"application_name empty":     func(data *providerModel) { data.ApplicationName = types.StringValue("") },
		"application_name non-ASCII": func(data *providerModel) { data.ApplicationName = types.StringValue("déploiement") },
		"application_name control":   func(data *providerModel) { data.ApplicationName = types.StringValue("line\nbreak") },
		"application_name too long":  func(data *providerModel) { data.ApplicationName = types.StringValue(strings.Repeat("a", 251)) },
		"unknown query_timeout":      func(data *providerModel) { data.QueryTimeout = types.StringUnknown() },
		"unknown connect_timeout":    func(data *providerModel) { data.Connection.ConnectTimeout = types.StringUnknown() },
		"unknown max_retries":        func(data *providerModel) { data.MaxRetries = types.Int64Unknown() },
		"unknown retry_mode":         func(data *providerModel) { data.RetryMode = types.StringUnknown() },
		"unknown application_name":   func(data *providerModel) { data.ApplicationName = types.StringUnknown() },
		"unknown with deferred client": func(data *providerModel) {
			data.QueryTimeout, data.Connection.Password = types.StringUnknown(), types.StringUnknown()
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := passwordProvider()
			change(&data)
			require.Error(t, data.validate(false))
			unknown := strings.HasPrefix(name, "unknown")
			if unknown {
				require.NoError(t, data.validate(true))
			} else {
				require.Error(t, data.validate(true))
			}
		})
	}
	data := passwordProvider()
	data.QueryTimeout, data.Connection.ConnectTimeout = types.StringValue("1h30m"), types.StringValue("500ms")
	data.MaxRetries, data.RetryMode = types.Int64Value(maxRetries), types.StringValue("adaptive")
	data.ApplicationName = types.StringValue(strings.Repeat("a", 250))
	require.NoError(t, data.validate(false))
}

// TestDurationValidator reports malformed durations as attribute errors and defers unknown values.
func TestDurationValidator(t *testing.T) {
	for value, valid := range map[types.String]bool{
		types.StringValue("30s"): true, types.StringNull(): true, types.StringUnknown(): true,
		types.StringValue("30"): false, types.StringValue("0s"): false,
	} {
		var resp validator.StringResponse
		durationValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("query_timeout"), ConfigValue: value}, &resp)
		assert.Equal(t, valid, !resp.Diagnostics.HasError(), value.String())
	}
	assert.NotEmpty(t, durationValidator{}.MarkdownDescription(context.Background()))
}

// TestAWSConfigRetrySettings converts max_retries to SDK attempts and leaves SDK defaults alone when omitted.
func TestAWSConfigRetrySettings(t *testing.T) {
	t.Setenv("AWS_MAX_ATTEMPTS", "")
	t.Setenv("AWS_RETRY_MODE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	data := providerModel{Region: types.StringValue("eu-central-1")}
	cfg, err := data.awsConfig(context.Background())
	require.NoError(t, err)
	assert.Zero(t, cfg.RetryMaxAttempts)
	assert.Empty(t, cfg.RetryMode)
	t.Setenv("AWS_MAX_ATTEMPTS", "7")
	t.Setenv("AWS_RETRY_MODE", "adaptive")
	cfg, err = data.awsConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.RetryMaxAttempts)
	assert.Equal(t, aws.RetryModeAdaptive, cfg.RetryMode)
	data.MaxRetries, data.RetryMode = types.Int64Value(0), types.StringValue("standard")
	cfg, err = data.awsConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.RetryMaxAttempts)
	assert.Equal(t, aws.RetryModeStandard, cfg.RetryMode)
	assert.Equal(t, 1, cfg.Retryer().MaxAttempts())
}

// TestRetryerHasNoRetryQuota keeps sustained throttling from exhausting retries that max_retries still allows: the
// SDK default quota refuses retries after 100 failures without an intervening success.
func TestRetryerHasNoRetryQuota(t *testing.T) {
	throttled := &smithy.GenericAPIError{Code: "ThrottlingException"}
	for _, mode := range []aws.RetryMode{"", aws.RetryModeStandard, aws.RetryModeAdaptive} {
		t.Run(string(mode), func(t *testing.T) {
			retryer := newRetryer(mode, maxRetries+1)
			assert.Equal(t, maxRetries+1, retryer.MaxAttempts())
			assert.True(t, retryer.IsErrorRetryable(throttled))
			for range 1000 {
				_, err := retryer.GetRetryToken(context.Background(), throttled)
				require.NoError(t, err)
			}
		})
	}
	assert.Equal(t, 3, newRetryer("", 0).MaxAttempts())
}
