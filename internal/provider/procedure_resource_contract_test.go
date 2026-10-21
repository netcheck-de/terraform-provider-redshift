package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestContractProcedureArgumentBlockPlans plans argument blocks through Terraform: no diff after create, including for a
// procedure without blocks, a lookup selecting the overload from the IN and INOUT blocks, an in-place update for a
// respelled argument, an import, and a replacement for a changed type.
func TestContractProcedureArgumentBlockPlans(t *testing.T) {
	t.Parallel()
	configuration := func(factor, factorType string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
resource "redshift_procedure" "scale" {
  database = "admin"
  schema   = "public"
  name     = "sp_plan_scale"
  argument {
    name = %q
    type = %q
  }
  argument {
    name = "amount"
    mode = "INOUT"
    type = "BIGINT"
  }
  argument {
    name = "label"
    mode = "OUT"
    type = "varchar(64)"
  }
  body = "BEGIN amount := amount * factor; label := 'scaled'; END;"
}
resource "redshift_procedure" "refresh" {
  database = "admin"
  schema   = "public"
  name     = "sp_plan_refresh"
  body     = "BEGIN NULL; END;"
}
data "redshift_procedure" "scale" {
  database  = redshift_procedure.scale.database
  schema    = redshift_procedure.scale.schema
  name      = redshift_procedure.scale.name
  arguments = [for argument in redshift_procedure.scale.argument : argument.type if argument.mode != "OUT"]
}`, factor, factorType)
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: fullCatalog()})},
		Steps: []testresource.TestStep{
			{Config: configuration("factor", "int"), Check: testresource.ComposeAggregateTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "signature", "INTEGER, BIGINT"),
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "argument.#", "3"),
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "argument.0.type", "int"),
				testresource.TestCheckResourceAttr("redshift_procedure.refresh", "argument.#", "0"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "arguments.#", "2"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.#", "3"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.0.type", "INTEGER"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.mode", "OUT"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.type", "CHARACTER VARYING"),
			)},
			{Config: configuration("FACTOR", "INTEGER"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_procedure.scale", plancheck.ResourceActionUpdate),
				plancheck.ExpectResourceAction("redshift_procedure.refresh", plancheck.ResourceActionNoop),
			}}},
			// The catalog keeps the folded name and the type without its length.
			{ResourceName: "redshift_procedure.scale", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"argument.0.name", "argument.2.type"}},
			{Config: configuration("factor", "bigint"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_procedure.scale", plancheck.ResourceActionDestroyBeforeCreate),
			}}, Check: testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "signature", "BIGINT, BIGINT")},
		},
	})
}
