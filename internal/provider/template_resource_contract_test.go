package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/assert"
)

// TestContractTemplateRenamePlan drives real plans: a rename updates in place, records the new identity, and converges.
func TestContractTemplateRenamePlan(t *testing.T) {
	t.Parallel()

	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(name, parameters string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_template" "csv" {
  database = "admin"
  schema = "serving"
  name = %q
  format = "CSV"
  parameters = %s
}`, name, parameters)
	}
	first := configuration("csv_load", `{ IGNOREHEADER = "1" }`)
	renamed := configuration("csv_import", `{ IGNOREHEADER = "1", GZIP = "" }`)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("csv_load", `{ MANIFEST = "" }`), ExpectError: regexp.MustCompile(`unsupported COPY parameter`), PlanOnly: true},
			{Config: first, Check: testresource.TestCheckResourceAttr("redshift_template.csv", "owner", "admin")},
			{
				Config: renamed,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_template.csv", plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue("redshift_template.csv", tfjsonpath.New("id")),
				}},
				Check: testresource.TestCheckResourceAttr("redshift_template.csv", "id", `{"database":"admin","name":"csv_import","schema":"serving"}`),
			},
		},
	})
	assert.Equal(t, "csv_import", fakeState[*templateFake](c, templateFakeFamily).name)
}
