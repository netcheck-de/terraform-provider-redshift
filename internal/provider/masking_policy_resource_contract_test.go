package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestContractMaskingPolicyBlockPlan drives input_column blocks through real plans, which the model-level tests skip: the
// required-block validator, the empty plan after apply, and the in-place update for another spelling of a type.
func TestContractMaskingPolicyBlockPlan(t *testing.T) {
	t.Parallel()

	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(blocks string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_masking_policy" "email" {
  database = "admin"
  name = "mask_email"
  expression = "'***'::VARCHAR(256)"
%s
}`, blocks)
	}
	varchar := configuration(`  input_column {
    name = "email"
    type = "VARCHAR(256)"
  }`)
	text := configuration(`  input_column {
    name = "email"
    type = "TEXT"
  }`)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration(""), ExpectError: regexp.MustCompile(`Block input_column must have a configuration value`), PlanOnly: true},
			{Config: varchar, Check: testresource.TestCheckResourceAttr("redshift_masking_policy.email", "input_column.0.type", "VARCHAR(256)")},
			{
				Config: text,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_masking_policy.email", plancheck.ResourceActionUpdate),
				}},
				Check: testresource.TestCheckResourceAttr("redshift_masking_policy.email", "input_column.0.type", "TEXT"),
			},
		},
	})
}
