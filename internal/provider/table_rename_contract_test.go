package provider

import (
	"regexp"
	"strings"
	"testing"

	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
)

// TestContractTableRenamePlans plans a renamed_from column as an in-place update, keeps the hint in state, and plans
// removing the hint in place too.
func TestContractTableRenamePlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")

	before := tableConfig(tableIDColumn+tableLabelColumn, "")
	after := strings.ReplaceAll(before, `"id"`, `"event_id"`)
	after = strings.Replace(after, `name     = "event_id"`, "name = \"event_id\"\n    renamed_from = \"id\"", 1)
	withoutHint := strings.Replace(after, "\n    renamed_from = \"id\"", "", 1)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: before},
			{Config: after, ConfigPlanChecks: tableExpectUpdate(), Check: testresource.TestCheckResourceAttr("redshift_table.events", "column.0.renamed_from", "id")},
			{Config: withoutHint, ConfigPlanChecks: tableExpectUpdate()},
		},
	})
}

// TestContractTableRenameConflictPlans fails the plan without writing when both the rename source and destination
// columns exist, and lets the prior configuration plan again.
func TestContractTableRenameConflictPlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")

	before := tableConfig(tableIDColumn+tableLabelColumn, "")
	after := strings.ReplaceAll(tableConfig(tableIDColumn, ""), `"id"`, `"label"`)
	after = strings.Replace(after, `name     = "label"`, "name = \"label\"\n    renamed_from = \"id\"", 1)
	resourceWrites := 0

	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: before, Check: func(_ *terraform.State) error {
				resourceWrites = len(c.writes)
				return nil
			}},
			{Config: after, PlanOnly: true, ExpectError: regexp.MustCompile("both source and destination exist")},
			{Config: before, PlanOnly: true, PreConfig: func() { assert.Len(t, c.writes, resourceWrites) }},
		},
	})
}

// A refreshed resource may legitimately contain the retained hint and a drift-created source column together.
// Treating that state as configuration used to fail decoding and request destructive table replacement.
func TestContractTableRenameRecreatedSourcePlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")

	before := tableConfig(tableIDColumn+tableLabelColumn, "")
	after := strings.ReplaceAll(before, `"id"`, `"event_id"`)
	after = strings.Replace(after, `name     = "event_id"`, "name = \"event_id\"\n    renamed_from = \"id\"", 1)
	changed := strings.Replace(after, `encoding = "LZO"`, `encoding = "ZSTD"`, 1)
	writes := 0

	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: before},
			{Config: after, ConfigPlanChecks: tableExpectUpdate()},
			{Config: changed, PlanOnly: true, ExpectNonEmptyPlan: true, ExpectError: regexp.MustCompile("both source and destination exist"), PreConfig: func() {
				writes = len(c.writes)
				table := fakeState[*tableFake](c, "table").tables["serving.events"]
				table.columns = append(table.columns, tableFakeColumn{name: "id", dataType: "bigint", encoding: "az64"})
			}},
			{Config: after, PlanOnly: true, ExpectError: regexp.MustCompile("both source and destination exist"), PreConfig: func() {
				assert.Len(t, c.writes, writes, "conflicting rename must fail before any replacement or drop/add")
			}},
		},
	})
}

// TestContractTableRenameAfterImportPlans renames a column of an imported table in place; only the test's final destroy
// drops the table.
func TestContractTableRenameAfterImportPlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	before := tableConfig(tableIDColumn+tableLabelColumn, "")
	imported := before + `
import {
  to = redshift_table.events
  id = jsonencode({ database = "admin", schema = "serving", name = "events" })
}
`
	after := strings.ReplaceAll(before, `"id"`, `"event_id"`)
	after = strings.Replace(after, `name     = "event_id"`, "name = \"event_id\"\n    renamed_from = \"id\"", 1)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: imported, ConfigPlanChecks: tableExpectUpdate()},
			{Config: after, ConfigPlanChecks: tableExpectUpdate(), Check: testresource.TestCheckResourceAttr("redshift_table.events", "column.0.renamed_from", "id")},
		},
	})
	assert.Equal(t, []string{
		`ALTER TABLE "serving"."events" RENAME COLUMN "id" TO "event_id"`,
		`DROP TABLE "serving"."events"`,
	}, c.writes, "import and its reconciliation must not replace the table")
}
