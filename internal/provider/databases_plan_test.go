package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// databasesPlanProvider is the provider block every plan-level test of this block shares.
const databasesPlanProvider = `
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
`

// databasesPlanCase applies before, then plans and applies after expecting an in-place update of address, and
// finally plans after again expecting no diff. The modifier tests call plan modifiers directly, so only a real
// plan shows computed attributes the framework marks unknown during an update.
func databasesPlanCase(t *testing.T, c *catalog, address, before, after string) {
	t.Helper()
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c}),
		},
		Steps: []testresource.TestStep{
			{Config: databasesPlanProvider + before},
			{Config: databasesPlanProvider + after, ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
			}}},
			{Config: databasesPlanProvider + after, PlanOnly: true},
		},
	})
}

// TestExternalSchemaInPlacePlans keeps in-place ALTER EXTERNAL SCHEMA changes from replacing a schema whose
// region is left to the warehouse.
func TestExternalSchemaInPlacePlans(t *testing.T) {
	glue := func(role, owner string) string {
		return fmt.Sprintf(`
resource "redshift_external_schema" "example" {
  database      = "admin"
  name          = "example_external"
  glue_database = "example_glue"
  iam_role_arn  = %q
  owner         = %q
}`, role, owner)
	}
	msk := func(uri string) string {
		return fmt.Sprintf(`
resource "redshift_external_schema" "example" {
  database       = "admin"
  name           = "example_external"
  source_type    = "MSK"
  authentication = "none"
  uri            = %q
}`, uri)
	}
	t.Run("glue role and owner", func(t *testing.T) {
		databasesPlanCase(t, &catalog{}, "redshift_external_schema.example",
			glue("arn:aws:iam::123456789012:role/spectrum", "admin"),
			glue("arn:aws:iam::123456789012:role/rotated", "etl"))
	})
	t.Run("msk uri", func(t *testing.T) {
		databasesPlanCase(t, &catalog{}, "redshift_external_schema.example",
			msk("b-1.example.kafka.eu-central-1.amazonaws.com:9092"),
			msk("b-2.example.kafka.eu-central-1.amazonaws.com:9092"))
	})
}

// TestDatabaseAndSchemaInPlacePlans keeps owner and option changes of databases and schemas in place.
func TestDatabaseAndSchemaInPlacePlans(t *testing.T) {
	database := func(owner string, limit int, isolation string) string {
		return fmt.Sprintf(`
resource "redshift_database" "warehouse" {
  name             = "warehouse"
  owner            = %q
  connection_limit = %d
  isolation_level  = %q
}`, owner, limit, isolation)
	}
	localSchema := func(owner string, quota int) string {
		return fmt.Sprintf(`
resource "redshift_schema" "serving" {
  database = "admin"
  name     = "serving"
  owner    = %q
  quota    = %d
}`, owner, quota)
	}
	t.Run("database", func(t *testing.T) {
		databasesPlanCase(t, &catalog{}, "redshift_database.warehouse", database("admin", -1, "SNAPSHOT"), database("etl", 0, "SERIALIZABLE"))
	})
	t.Run("schema", func(t *testing.T) {
		databasesPlanCase(t, &catalog{}, "redshift_schema.serving", localSchema("admin", -1), localSchema("etl", 2048))
	})
}
