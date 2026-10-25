package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
)

// TestContractViewImportPlansInPlaceReplace runs Terraform against the fake catalog: after an import, the catalog text
// differs from the configured query, so the plan updates the view in place, and the next plan is empty.
func TestContractViewImportPlansInPlaceReplace(t *testing.T) {
	t.Parallel()

	c, views := viewFake()
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_view" "labels" {
  database = "admin"
  schema = %q
  name = %q
  query = %q
}`, fakeViewSchema, fakeViewName, "SELECT id,\n  label FROM serving.sales")
	address := "redshift_view.labels"
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{
				Config: configuration, ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"database":"admin","schema":%q,"name":%q}`, fakeViewSchema, fakeViewName),
			},
			{
				Config:           configuration,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check: func(*terraform.State) error {
					c.mu.Lock()
					defer c.mu.Unlock()

					assert.Equal(t, "SELECT id,\n  label FROM serving.sales", views.get(fakeViewName).query)
					assert.Equal(t, fakeViewOwner, views.get(fakeViewName).owner, "CREATE OR REPLACE VIEW keeps the owner")

					return nil
				},
			},
		},
	})
	assert.Nil(t, views.get(fakeViewName), "destroy drops the view")
}
