package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestContractProviderLifecycle exercises creation, imports, drift repair, replacement, and dependency-ordered cleanup.
func TestContractProviderLifecycle(t *testing.T) {
	t.Parallel()

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
				checkAttributes(t, "data.redshift_database.analytics", map[string]string{"database_type": "SHARED", "datashare_arn": shareARN, "id": `{"datashare_arn":"` + shareARN + `","name":"analytics"}`}),
				checkAttributes(t, "data.redshift_datashare.producer", map[string]string{"publicly_accessible": "false"}),
				checkAttributes(t, "redshift_grant.read", map[string]string{"privileges.#": "1"}),
			)},
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
		},
	})
}

// TestContractFreshWorkgroupProviderConfiguration checks a warehouse that is unknown until apply.
func TestContractFreshWorkgroupProviderConfiguration(t *testing.T) {
	t.Parallel()

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
