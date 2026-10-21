package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestContractReferencedRoleNameChangeReplacesMembership verifies Terraform propagates names without caller lifecycle triggers.
func TestContractReferencedRoleNameChangeReplacesMembership(t *testing.T) {
	t.Parallel()
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(name string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_role_grant" "reader" {
  role = "sys:dba"
  to_role = redshift_role.reader.name
}`, name)
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("before")},
			{Config: configuration("after"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_role.reader", plancheck.ResourceActionDestroyBeforeCreate),
				plancheck.ExpectResourceAction("redshift_role_grant.reader", plancheck.ResourceActionDestroyBeforeCreate),
			}}, Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_role.reader", "name", "after"),
				testresource.TestCheckResourceAttr("redshift_role_grant.reader", "to_role", "after"),
			)},
		},
	})
}
