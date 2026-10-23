package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/stretchr/testify/assert"
)

// TestContractMaterializedViewImportAdoptionPlans runs Terraform against the fake catalog: an imported view adopts its
// configuration in place, after which changing backup replaces it while the distribution and sort key change in
// place.
func TestContractMaterializedViewImportAdoptionPlans(t *testing.T) {
	t.Parallel()
	c := fullCatalog()
	configuration := func(options string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_materialized_view" "summary" {
  database = "admin"
  schema = %q
  name = %q
  auto_refresh = true
  query = %q
  %s
}`, fakeViewSchema, fakeMaterializedViewName, fakeMaterializedQuery, options)
	}
	address := "redshift_materialized_view.summary"
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{
				Config: configuration(`backup = false`), ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":%q,"name":%q}`, fakeViewSchema, fakeMaterializedViewName),
			},
			{Config: configuration(`backup = false`), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.TestCheckResourceAttr(address, "query", fakeMaterializedQuery)},
			{Config: configuration(`backup = true`), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
			{Config: configuration("backup = true\n  sort_key {\n    columns = [\"label\"]\n  }"), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate)},
			{Config: configuration("backup = true\n  distribution {\n    style = \"ALL\"\n  }"), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate)},
		},
	})
}

// TestContractMaterializedViewStorageBlockPlans runs Terraform against the fake catalog: a view created with the
// distribution and sort_key blocks plans no change afterwards, changing or removing the blocks alters the view in
// place, and an empty distribution block is rejected before apply.
func TestContractMaterializedViewStorageBlockPlans(t *testing.T) {
	t.Parallel()
	c := fullCatalog()
	configuration := func(storage string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_materialized_view" "rollup" {
  database = "admin"
  schema = %q
  name = "sales_rollup"
  query = %q
  %s
}`, fakeViewSchema, fakeMaterializedQuery, storage)
	}
	pendingOwner := "\nresource \"terraform_data\" \"owner\" {}"
	address := "redshift_materialized_view.rollup"
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	keyed := configuration(`distribution {
    key = "label"
  }
  sort_key {
    columns = ["label"]
  }`)
	even := configuration(`distribution {
    style = "EVEN"
  }
  sort_key {
    columns = ["label", "sales"]
  }`)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: keyed, Check: testresource.ComposeAggregateTestCheckFunc(
				testresource.TestCheckResourceAttr(address, "distribution.key", "label"),
				testresource.TestCheckNoResourceAttr(address, "distribution.style"),
				testresource.TestCheckResourceAttr(address, "sort_key.columns.#", "1"),
			)},
			{Config: even, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate)},
			{Config: configuration("distribution {}"), PlanOnly: true, ExpectError: regexp.MustCompile(`requires a style or a key`)},
			{Config: configuration("sort_key {}"), PlanOnly: true, ExpectError: regexp.MustCompile(`requires columns`)},
			// An unknown owner leaves the configuration partly unknown, which must not defer the empty-block checks to apply.
			{Config: configuration("owner = terraform_data.owner.id\n  distribution {}") + pendingOwner, PlanOnly: true, ExpectError: regexp.MustCompile(`requires a style or a key`)},
			{Config: configuration("owner = terraform_data.owner.id\n  sort_key {}") + pendingOwner, PlanOnly: true, ExpectError: regexp.MustCompile(`requires columns`)},
			// The last configuration is also the one the harness destroys with, so it must stay valid.
			{Config: configuration(""), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.ComposeAggregateTestCheckFunc(
				testresource.TestCheckNoResourceAttr(address, "distribution.style"),
				testresource.TestCheckNoResourceAttr(address, "sort_key.columns.#"),
			)},
		},
	})
	var storage []string
	for _, write := range c.writes {
		if strings.HasPrefix(write, `ALTER MATERIALIZED VIEW "serving"."sales_rollup"`) {
			storage = append(storage, strings.TrimPrefix(write, `ALTER MATERIALIZED VIEW "serving"."sales_rollup" `))
		}
	}
	assert.Equal(t, []string{`ALTER DISTSTYLE EVEN`, `ALTER COMPOUND SORTKEY ("label", "sales")`, `ALTER DISTSTYLE EVEN`, `ALTER SORTKEY NONE`}, storage)
}
