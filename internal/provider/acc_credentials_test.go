package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/stretchr/testify/require"
)

// TestAcceptanceCredentials verifies that fixture clients and the provider inherit the same ambient AWS identity.
func TestAcceptanceCredentials(t *testing.T) {
	directory := t.TempDir()
	credentials := filepath.Join(directory, "credentials")
	require.NoError(t, os.WriteFile(credentials, []byte("[default]\naws_access_key_id = DEFAULT\naws_secret_access_key = example\n[local]\naws_access_key_id = LOCAL\naws_secret_access_key = example\n"), 0o600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentials)
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(directory, "absent-config"))
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_ROLE_ARN", "")

	for _, test := range []struct{ name, profile, accessKey, want string }{
		{"default profile", "", "", "DEFAULT"},
		{"active profile", "local", "", "LOCAL"},
		{"CI environment", "", "CI", "CI"},
		{"environment overrides profile", "local", "CI", "CI"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AWS_PROFILE", test.profile)
			t.Setenv("AWS_ACCESS_KEY_ID", test.accessKey)
			t.Setenv("AWS_SECRET_ACCESS_KEY", "")
			t.Setenv("AWS_SESSION_TOKEN", "")

			if test.accessKey != "" {
				t.Setenv("AWS_SECRET_ACCESS_KEY", "example")
				t.Setenv("AWS_SESSION_TOKEN", "session")
			}

			p := New("test")().(*redshiftProvider)

			var response framework.ConfigureResponse

			p.Configure(context.Background(), framework.ConfigureRequest{Config: providerConfig(t, p, types.StringValue("eu-central-1"))}, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)

			for _, client := range []*dataapi.Client{testAccRolesClient(t, "eu-central-1", "fixture").(*dataapi.Client), p.client.(*dataapi.Client)} {
				identity, err := client.API.(*redshiftdata.Client).Options().Credentials.Retrieve(context.Background())
				require.NoError(t, err)
				require.Equal(t, test.want, identity.AccessKeyID)
			}
		})
	}
}
