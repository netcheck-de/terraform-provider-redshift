package provider

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccTableLifecycle creates tables in an isolated database, applies every in-place change kind, checks that
// catalog reads plan no drift, imports the table, replaces it for a narrowed column, and destroys it.
func TestAccTableLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_table_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(note, attributes string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "serving" {
  database = redshift_database.local.name
  name     = "serving"
}
resource "redshift_table" "accounts" {
  database    = redshift_schema.serving.database
  schema      = redshift_schema.serving.name
  name        = "accounts"
  columns     = [{ name = "account_id", type = "int", nullable = false }]
  primary_key = ["account_id"]
}
resource "redshift_table" "events" {
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "events"
  columns = [
    { name = "id", type = "int8", identity = { seed = 1, step = 1 } },
    { name = "account_id", type = "integer", nullable = false, encoding = "AZ64" },
    { name = "label", type = "varchar", default = "'n/a'" },%s
  ]
  primary_key = ["id"]%s
}
data "redshift_table" "events" {
  database = redshift_table.events.database
  schema   = redshift_table.events.schema
  name     = redshift_table.events.name
}
`, region, profile, workgroup, database, name, note, attributes)
	}
	inPlace := func(note, attributes string) resource.TestStep {
		return resource.TestStep{Config: configuration(note, attributes), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
			plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
		}}}
	}
	keys := `
  distkey = "account_id"
  sortkey = ["id"]`
	foreignKey := keys + `
  foreign_keys = [{ columns = ["account_id"], references_schema = "serving", references_table = redshift_table.accounts.name, references_columns = ["account_id"] }]`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("acc")())},
		Steps: []resource.TestStep{
			{Config: configuration("", keys), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_table.events", "columns.0.type", "bigint"),
				resource.TestCheckResourceAttr("data.redshift_table.events", "columns.2.type", "character varying(256)"),
				resource.TestCheckResourceAttr("data.redshift_table.events", "diststyle", "KEY"),
			)},
			{Config: configuration("", keys), PlanOnly: true},
			inPlace(`
    { name = "note", type = "varchar(32)", encoding = "LZO" },`, keys),
			inPlace(`
    { name = "note", type = "varchar(64)", encoding = "ZSTD" },`, foreignKey+`
  unique = [["account_id", "id"]]`),
			inPlace(`
    { name = "note", type = "varchar(64)", encoding = "ZSTD" },`, `
  diststyle = "EVEN"
  sortkey_style = "AUTO"`),
			inPlace("", keys),
			{ResourceName: "redshift_table.events", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"columns.0.type", "columns.2.type", "columns.2.default", "backup"}},
			{Config: configuration(`
    { name = "note", type = "varchar(16)" },`, keys), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}},
			{Config: configuration(`
    { name = "note", type = "varchar(8)" },`, keys), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionDestroyBeforeCreate),
			}}},
		},
	})
}
