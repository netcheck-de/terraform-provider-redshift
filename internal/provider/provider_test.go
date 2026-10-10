package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	framework "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// testConfig composes the provider's SQL objects for offline Terraform lifecycle tests.
func testConfig(privileges, iamRole string) string {
	return fmt.Sprintf(`
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
resource "redshift_identity_provider" "main" {
  name            = "identity"
  namespace       = "example"
  application_arn = "application"
  iam_role_arn    = %q
}

resource "redshift_role" "readers" {
  name       = "${redshift_identity_provider.main.namespace}:readers"

}

resource "redshift_role_grant" "admin" {
  role    = "sys:dba"
  to_role = redshift_role.readers.name

}

resource "redshift_user" "grafana" {
  name        = "grafana"
  password_wo = "TestPass1234"
}

resource "redshift_role_grant" "monitor" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}

resource "redshift_database" "analytics" {
  name          = "analytics"
  datashare_arn = %q
}

resource "redshift_database" "producer" {
  name   = "warehouse"
}

resource "redshift_datashare" "producer" {
  database = redshift_database.producer.name
  name     = "producer"
}

resource "redshift_schema" "serving" {
  database = redshift_database.producer.name
  name     = "serving"
}

resource "redshift_external_schema" "glue" {
  database      = redshift_database.producer.name
  name          = "example_external"
  glue_database = "example_glue"
  iam_role_arn  = "arn:aws:iam::123456789012:role/spectrum"
}

resource "redshift_datashare_schema" "serving" {
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name
  schema    = redshift_schema.serving.name
  include_new = true
}

resource "redshift_datashare_table" "table" {
  database  = redshift_datashare_schema.serving.database
  datashare = redshift_datashare_schema.serving.datashare
  schema    = redshift_datashare_schema.serving.schema
  table     = "table"
}

resource "redshift_datashare_grant" "consumer" {
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name
  account_id = "123456789012"
}

resource "redshift_grant" "read" {
  database_name = redshift_database.analytics.name
  role          = redshift_role.readers.name
  scope         = "TABLES"
  privileges    = %s

}

data "redshift_user" "grafana" {
  name = redshift_user.grafana.name
}

data "redshift_role" "readers" {
  name = redshift_role.readers.name
}

data "redshift_identity_provider" "main" {
  name = redshift_identity_provider.main.name
}

data "redshift_database" "analytics" {
  name = redshift_database.analytics.name
}

data "redshift_datashare" "producer" {
  database = redshift_datashare.producer.database
  name     = redshift_datashare.producer.name
}

data "redshift_schema" "serving" {
  database = redshift_schema.serving.database
  name     = redshift_schema.serving.name
}

data "redshift_external_schema" "glue" {
  database = redshift_external_schema.glue.database
  name     = redshift_external_schema.glue.name
}
`, iamRole, shareARN, privileges)
}

// checkAttributes compares selected Terraform state attributes using Testify assertions.
func checkAttributes(t *testing.T, name string, expected map[string]string) resource.TestCheckFunc {
	t.Helper()
	return func(state *terraform.State) error {
		data := state.RootModule().Resources[name]
		require.NotNil(t, data, "resource %s", name)
		require.NotNil(t, data.Primary, "resource %s", name)
		for key, value := range expected {
			assert.Equal(t, value, data.Primary.Attributes[key], "%s.%s", name, key)
		}
		return nil
	}
}

// TestProviderLifecycle exercises creation, imports, drift repair, replacement, and dependency-ordered cleanup.
func TestProviderLifecycle(t *testing.T) {
	c := &catalog{privileges: map[string]bool{}}
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c, datashareARN: testDatashareARN}),
	}
	config := testConfig(`["SELECT"]`, "role-one")
	replacement := strings.Replace(testConfig(`["SELECT", "INSERT"]`, "role-two"), "datashare_arn =", "with_permissions = false\n  datashare_arn =", 1)
	replacement = strings.Replace(replacement, `name          = "analytics"`, `name          = "analytics_after"`, 1)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			assert.False(t, c.role)
			assert.False(t, c.membership)
			assert.False(t, c.userGrant)
			assert.False(t, c.user)
			assert.False(t, c.database)
			assert.False(t, c.localDB)
			assert.False(t, c.share)
			assert.False(t, c.schema)
			assert.False(t, c.external)
			assert.False(t, c.shareSchema)
			assert.False(t, c.shareTable)
			assert.False(t, c.shareGrant)
			assert.False(t, c.identity)
			assert.Empty(t, c.privileges)
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.ComposeTestCheckFunc(
				checkAttributes(t, "redshift_database.analytics", map[string]string{"with_permissions": "true", "database_type": "SHARED", "share_name": "source", "producer_account": "123456789012", "producer_namespace": "11111111-2222-3333-4444-555555555555"}),
				checkAttributes(t, "redshift_database.producer", map[string]string{"name": "warehouse"}),
				checkAttributes(t, "redshift_datashare.producer", map[string]string{"publicly_accessible": "false"}),
				checkAttributes(t, "redshift_datashare_schema.serving", map[string]string{"include_new": "true"}),
				checkAttributes(t, "data.redshift_schema.serving", map[string]string{"owner": "admin"}),
				checkAttributes(t, "data.redshift_external_schema.glue", map[string]string{"glue_database": "example_glue"}),
				checkAttributes(t, "redshift_identity_provider.main", map[string]string{"enabled": "true"}),
				checkAttributes(t, "redshift_user.grafana", map[string]string{"password_wo_version": "0"}),
				checkAttributes(t, "data.redshift_user.grafana", map[string]string{"superuser": "false"}),
				checkAttributes(t, "data.redshift_database.analytics", map[string]string{"database_type": "SHARED", "datashare_arn": shareARN, "id": `{"database":"admin","datashare_arn":"` + shareARN + `","name":"analytics","workgroup_name":"warehouse"}`}),
				checkAttributes(t, "data.redshift_datashare.producer", map[string]string{"publicly_accessible": "false"}),
				checkAttributes(t, "redshift_grant.read", map[string]string{"privileges.#": "1"}),
			)},
			{Config: config, PlanOnly: true},
			{ResourceName: "redshift_role.readers", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_database.analytics", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_database.producer", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare.producer", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_schema.serving", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_external_schema.glue", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare_schema.serving", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare_table.table", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare_grant.consumer", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_identity_provider.main", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_role_grant.admin", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_user.grafana", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_role_grant.monitor", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.read", ImportState: true, ImportStateVerify: true},
			{
				Config: config, PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					c.mu.Lock()
					defer c.mu.Unlock()
					c.privileges["INSERT"] = true
					c.enabled = false
					c.writes = nil
				},
			},
			{
				Config: config,
				PreConfig: func() {
					c.mu.Lock()
					defer c.mu.Unlock()
					require.Empty(t, c.writes, "plan must not perform SQL mutations")
				},
				Check: func(*terraform.State) error {
					c.mu.Lock()
					defer c.mu.Unlock()
					assert.True(t, c.enabled)
					assert.Equal(t, map[string]bool{"SELECT": true}, c.privileges)
					assert.Contains(t, c.writes, `REVOKE INSERT FOR TABLES IN DATABASE "analytics" FROM ROLE "example:readers"`)
					return nil
				},
			},
			{
				Config: testConfig(`["SELECT", "INSERT"]`, "role-two"),
				Check: resource.ComposeTestCheckFunc(
					checkAttributes(t, "redshift_grant.read", map[string]string{"privileges.#": "2"}),
					checkAttributes(t, "redshift_identity_provider.main", map[string]string{"iam_role_arn": "role-two"}),
				),
			},
			{
				Config: testConfig(`["SELECT", "INSERT"]`, "role-two"),
				PreConfig: func() {
					c.mu.Lock()
					defer c.mu.Unlock()
					c.role, c.membership = false, false
					clear(c.privileges)
				},
				Check: checkAttributes(t, "redshift_grant.read", map[string]string{"privileges.#": "2"}),
			},
			{
				Config: replacement,
				Check: resource.ComposeTestCheckFunc(
					checkAttributes(t, "redshift_database.analytics", map[string]string{"with_permissions": "false"}),
					checkAttributes(t, "redshift_grant.read", map[string]string{"privileges.#": "2"}),
				),
			},
			{
				Config:   replacement,
				PlanOnly: true,
			},
		},
	})
}

// TestFreshWorkgroupProviderConfiguration checks a warehouse that is unknown until apply.
func TestFreshWorkgroupProviderConfiguration(t *testing.T) {
	c := &catalog{identity: true, enabled: true, iamRole: "role-one"}
	config := `
resource "terraform_data" "workgroup" {}
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = terraform_data.workgroup.id
  database       = "admin"
}
resource "redshift_role" "readers" {
  name = "example:readers"
}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c}),
		},
		Steps: []resource.TestStep{{Config: config}},
	})
}
