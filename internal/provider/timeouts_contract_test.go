package provider

import (
	"regexp"
	"testing"

	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
)

// TestContractTableTimeoutsPlan keeps timeout-only changes in place without issuing DDL.
func TestContractTableTimeoutsPlan(t *testing.T) {
	t.Parallel()
	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")
	lookup := `
data "redshift_table" "events" {
  database = redshift_table.events.database
  schema   = redshift_table.events.schema
  name     = redshift_table.events.name
}
`
	config := func(timeouts string) string {
		return tableConfig(tableIDColumn+tableLabelColumn, `
  timeouts {`+timeouts+`
  }`) + lookup
	}
	var applied int
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: config(`
    create = "10m"`), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.create", "10m"),
				testresource.TestCheckNoResourceAttr("redshift_table.events", "timeouts.update"),
				testresource.TestCheckNoResourceAttr("data.redshift_table.events", "timeouts.%"),
				testresource.TestCheckResourceAttr("data.redshift_table.events", "column.0.name", "id"),
			)},
			{Config: config(`
    create = "10m"
    update = "5m"
    delete = "2m"`), PreConfig: func() { applied = len(c.writes) }, ConfigPlanChecks: tableExpectUpdate(), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.update", "5m"),
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.delete", "2m"),
				func(*terraform.State) error {
					assert.Len(t, c.writes, applied, "changing timeouts runs no DDL")
					return nil
				},
			)},
			{Config: config(`
    update = "0s"`), PlanOnly: true, ExpectError: regexp.MustCompile(`must be positive`)},
			{Config: config(`
    read = "1m"`), PlanOnly: true, ExpectError: regexp.MustCompile(`Unsupported argument`)},
			{Config: config(""), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}, Check: testresource.TestCheckNoResourceAttr("redshift_table.events", "timeouts.create")},
		},
	})
}
