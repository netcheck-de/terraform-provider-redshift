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

// TestAccTableLifecycle creates tables in an isolated database, applies every in-place change kind, including a
// column inserted between others, reordered columns, and columns dropped from the middle of both the configured and
// the physical order, checks that catalog reads plan no drift, imports the table, replaces it for a narrowed column,
// and destroys it.
func TestAccTableLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_table_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	const (
		idColumn = `
  column {
    name = "id"
    type = "int8"
    identity {
      seed = 1
      step = 1
    }
  }`
		accountColumn = `
  column {
    name     = "account_id"
    type     = "integer"
    nullable = false
    encoding = "AZ64"
  }`
		labelColumn = `
  column {
    name    = "label"
    type    = "varchar"
    default = "'n/a'"
  }`
	)
	note := func(dataType, encoding string) string {
		text := fmt.Sprintf(`
  column {
    name = "note"
    type = %q`, dataType)
		if encoding != "" {
			text += fmt.Sprintf(`
    encoding = %q`, encoding)
		}
		return text + `
  }`
	}
	configuration := func(columns, attributes string) string {
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
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "accounts"
  column {
    name     = "account_id"
    type     = "int"
    nullable = false
  }
  primary_key {
    columns = ["account_id"]
  }
}
resource "redshift_table" "events" {
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "events"
%s
  primary_key {
    columns = ["id"]
  }%s
}
data "redshift_table" "events" {
  database = redshift_table.events.database
  schema   = redshift_table.events.schema
  name     = redshift_table.events.name
}
`, region, profile, workgroup, database, name, columns, attributes)
	}
	inPlace := func(columns, attributes string) resource.TestStep {
		return resource.TestStep{Config: configuration(columns, attributes), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
			plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
		}}}
	}
	base := idColumn + accountColumn + labelColumn
	keys := `
  distribution {
    key = "account_id"
  }
  sort_key {
    columns = ["id"]
  }`
	constraints := keys + `
  foreign_key {
    columns = ["account_id"]
    references {
      schema  = "serving"
      table   = redshift_table.accounts.name
      columns = ["account_id"]
    }
  }
  unique {
    columns = ["account_id", "id"]
  }`
	auto := `
  distribution {
    style = "EVEN"
  }
  sort_key {
    style = "AUTO"
  }`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("acc")())},
		Steps: []resource.TestStep{
			{Config: configuration(base, keys), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_table.events", "column.0.type", "bigint"),
				resource.TestCheckResourceAttr("data.redshift_table.events", "column.2.type", "character varying(256)"),
				resource.TestCheckResourceAttr("data.redshift_table.events", "distribution.style", "KEY"),
				resource.TestCheckResourceAttr("redshift_table.events", "effective_distribution.key", "account_id"),
				resource.TestCheckResourceAttr("redshift_table.events", "effective_sort_key.auto", "false"),
			)},
			{Config: configuration(base, keys), PlanOnly: true},
			inPlace(idColumn+note("varchar(32)", "LZO")+accountColumn+labelColumn, keys),
			{Config: configuration(idColumn+note("varchar(32)", "LZO")+accountColumn+labelColumn, keys), PlanOnly: true},
			inPlace(labelColumn+idColumn+accountColumn+note("varchar(64)", "ZSTD"), constraints),
			inPlace(idColumn+labelColumn+accountColumn+note("varchar(64)", "ZSTD"), auto),
			// label sits between id and account_id in the configuration and before note in the table.
			inPlace(idColumn+accountColumn+note("varchar(64)", "ZSTD"), keys),
			{Config: configuration(idColumn+accountColumn+note("varchar(64)", "ZSTD"), keys), PlanOnly: true},
			// Redshift appends label after note, so dropping note removes a column with one stored after it.
			inPlace(base, keys),
			{Config: configuration(base, keys), PlanOnly: true},
			{ResourceName: "redshift_table.events", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"column.0.type", "column.2.type", "column.2.default", "backup"}},
			{Config: configuration(base+note("varchar(16)", ""), keys), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}},
			{Config: configuration(base+note("varchar(8)", ""), keys), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionDestroyBeforeCreate),
			}}},
		},
	})
}
