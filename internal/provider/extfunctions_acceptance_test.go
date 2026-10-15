package provider

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccExternalFunctionLifecycle creates a Lambda UDF in a disposable database, restates it in place, lists it with
// the routine lookups, imports it, and drops it with the database. REDSHIFT_ACC_LAMBDA_FUNCTION names an existing
// Lambda function; REDSHIFT_ACC_LAMBDA_ROLE optionally names an associated role ARN instead of the default role.
func TestAccExternalFunctionLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_LAMBDA_FUNCTION")
	role := os.Getenv("REDSHIFT_ACC_LAMBDA_ROLE")
	if role == "" {
		role = "DEFAULT"
	}
	name := "acc_exfunc_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(retryTimeout int, volatility string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_external_function" "upper" {
  database        = redshift_database.local.name
  schema          = "public"
  name            = "f_acc_upper"
  arguments  = ["varchar", "int"]
  return_type     = "varchar"
  volatility      = %q
  lambda_function = %q
  iam_role        = %q
  retry_timeout   = %d
}
data "redshift_functions" "upper" {
  database = redshift_external_function.upper.database
  schema   = redshift_external_function.upper.schema
  name     = redshift_external_function.upper.name
}
data "redshift_routine_parameters" "upper" {
  database     = redshift_external_function.upper.database
  schema       = redshift_external_function.upper.schema
  routine_name = redshift_external_function.upper.name
}
`, region, profile, workgroup, database, name, volatility, os.Getenv("REDSHIFT_ACC_LAMBDA_FUNCTION"), role, retryTimeout)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(0, "VOLATILE"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_external_function.upper", "volatility", "VOLATILE"),
				resource.TestCheckResourceAttrSet("redshift_external_function.upper", "owner"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.0.language", "EXFUNC"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.0.arguments.0", "CHARACTER VARYING"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.0.arguments.1", "INTEGER"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.0.return_type", "CHARACTER VARYING"),
			)},
			{Config: configuration(3000, "STABLE"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_external_function.upper", "volatility", "STABLE"),
				resource.TestCheckResourceAttr("data.redshift_functions.upper", "functions.0.volatility", "STABLE"),
			)},
			{Config: configuration(3000, "STABLE"), PlanOnly: true},
			{
				ResourceName: "redshift_external_function.upper", ImportState: true, ImportStateVerify: true,
				// No documented catalog view reports the Lambda options, and the catalog spells the types canonically.
				ImportStateVerifyIgnore: []string{"lambda_function", "iam_role", "retry_timeout", "arguments", "return_type"},
			},
		},
	})
}
