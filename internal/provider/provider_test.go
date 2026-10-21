package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	transport "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shareARN identifies the synthetic producer share used in offline lifecycle tests.
const shareARN = "arn:aws:redshift:eu-central-1:123456789012:datashare:11111111-2222-3333-4444-555555555555/source"

// testDatashareARN provides producer metadata without AWS calls during offline lifecycle tests.
func testDatashareARN(context.Context, shareSource) (string, error) {
	return shareARN, nil
}

// providerConfig builds typed provider configuration using the default AWS credential chain.
func providerConfig(t *testing.T, p *redshiftProvider, region types.String) tfsdk.Config {
	t.Helper()
	return providerConfigWithProfile(t, p, region, types.StringNull())
}

// providerConfigWithProfile builds typed configuration for explicit profile-selection tests.
func providerConfigWithProfile(t *testing.T, p *redshiftProvider, region, profile types.String) tfsdk.Config {
	t.Helper()
	var schema framework.SchemaResponse
	p.Schema(context.Background(), framework.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	diagnostics := state.Set(context.Background(), &providerModel{Region: region, Profile: profile, Workgroup: types.StringValue("warehouse"), Database: types.StringValue("admin")})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	return tfsdk.Config(state)
}

// TestProviderConfiguration checks metadata, unknown configuration, region resolution, and profile isolation.
func TestProviderConfiguration(t *testing.T) {
	p := New("test")().(*redshiftProvider)
	var metadata framework.MetadataResponse
	p.Metadata(context.Background(), framework.MetadataRequest{}, &metadata)
	assert.Equal(t, "redshift", metadata.TypeName)
	assert.Equal(t, "test", metadata.Version)
	for _, invalid := range []string{"model", "unknown region", "unknown profile"} {
		t.Run(invalid, func(t *testing.T) {
			config := providerConfig(t, p, types.StringUnknown())
			switch invalid {
			case "model":
				config.Raw = tftypes.NewValue(tftypes.String, "invalid model")
			case "unknown profile":
				config = providerConfigWithProfile(t, p, types.StringValue("eu-central-1"), types.StringUnknown())
			}
			var resp framework.ConfigureResponse
			p.Configure(context.Background(), framework.ConfigureRequest{Config: config}, &resp)
			assert.True(t, resp.Diagnostics.HasError())
		})
	}
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent-credentials"))
	t.Run("missing region", func(t *testing.T) {
		var resp framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, types.StringNull())}, &resp)
		assert.True(t, resp.Diagnostics.HasError())
	})
	t.Run("invalid AWS configuration", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config")
		require.NoError(t, os.WriteFile(path, []byte("[profile broken]\nrole_arn = arn:aws:iam::111111111111:role/example\nsource_profile = missing\n"), 0o600))
		t.Setenv("AWS_CONFIG_FILE", path)
		t.Setenv("AWS_PROFILE", "broken")
		var resp framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, types.StringValue("eu-central-1"))}, &resp)
		assert.True(t, resp.Diagnostics.HasError())
	})
	t.Run("explicit region", func(t *testing.T) {
		var resp framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, types.StringValue("eu-central-1"))}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		assert.NotNil(t, resp.ResourceData)
	})
	t.Run("SDK region", func(t *testing.T) {
		t.Setenv("AWS_REGION", "eu-central-1")
		p := &redshiftProvider{}
		var resp framework.ConfigureResponse
		p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, types.StringNull())}, &resp)
		assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	})
	t.Run("separate shared profiles", func(t *testing.T) {
		credentials := filepath.Join(t.TempDir(), "credentials")
		require.NoError(t, os.WriteFile(credentials, []byte("[producer]\naws_access_key_id = PRODUCER\naws_secret_access_key = example\n[consumer]\naws_access_key_id = CONSUMER\naws_secret_access_key = example\n"), 0o600))
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentials)
		t.Setenv("AWS_PROFILE", "producer")
		for profile, accessKey := range map[string]string{"producer": "PRODUCER", "consumer": "CONSUMER"} {
			t.Run(profile, func(t *testing.T) {
				p := New("test")().(*redshiftProvider)
				var resp framework.ConfigureResponse
				p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfigWithProfile(t, p, types.StringValue("eu-central-1"), types.StringValue(profile))}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
				client := p.client.(*transport.Client).API.(*redshiftdata.Client)
				identity, err := client.Options().Credentials.Retrieve(context.Background())
				require.NoError(t, err)
				assert.Equal(t, accessKey, identity.AccessKeyID)
			})
		}
	})
}
