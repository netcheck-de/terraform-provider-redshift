package provider

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
)

// TestContractTablePlans runs real plans against the fake: derived defaults plan no drift, a column inserted between
// others and reordered columns update in place without inconsistent results, and widened and narrowed columns update
// in place.
func TestContractTablePlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")

	var reordered int

	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: tableConfig(tableIDColumn+tableLabelColumn, ""), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "distribution.style", "KEY"),
				testresource.TestCheckResourceAttr("redshift_table.events", "sort_key.style", "COMPOUND"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_distribution.key", "id"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_distribution.auto", "false"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_sort_key.columns.0", "id"),
				testresource.TestCheckResourceAttr("redshift_table.events", "owner", "admin"),
				testresource.TestCheckResourceAttr("redshift_table.events", "column.0.nullable", "false"),
				testresource.TestCheckResourceAttr("redshift_table.events", "column.0.identity.generated_by_default", "false"),
				testresource.TestCheckResourceAttr("redshift_table.events", "column.1.nullable", "true"),
				testresource.TestCheckResourceAttr("redshift_table.events", "column.1.default", "'none'"),
				testresource.TestCheckNoResourceAttr("redshift_table.events", "column.1.identity.seed"),
			)},
			{
				Config: tableConfig(tableIDColumn+tableNoteColumn("varchar(32)")+tableLabelColumn, ""), ConfigPlanChecks: tableExpectUpdate(),
				Check: testresource.ComposeTestCheckFunc(
					testresource.TestCheckResourceAttr("redshift_table.events", "column.1.name", "note"),
					testresource.TestCheckResourceAttr("redshift_table.events", "column.1.encoding", "LZO"),
					testresource.TestCheckResourceAttr("redshift_table.events", "column.2.name", "label"),
				),
			},
			{
				Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(32)"), ""), ConfigPlanChecks: tableExpectUpdate(),
				PreConfig: func() { reordered = len(c.writes) },
				Check: testresource.ComposeTestCheckFunc(
					testresource.TestCheckResourceAttr("redshift_table.events", "column.0.name", "label"),
					func(*terraform.State) error {
						if len(c.writes) != reordered {
							return fmt.Errorf("reordering columns wrote %v", c.writes[reordered:])
						}

						return nil
					},
				),
			},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(128)"), `
  owner = "analyst"`), ConfigPlanChecks: tableExpectUpdate(), Check: testresource.TestCheckResourceAttr("redshift_table.events", "owner", "analyst")},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(16)"), `
  owner = "analyst"`), ConfigPlanChecks: tableExpectUpdate()},
		},
	})
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ADD COLUMN "note" CHARACTER VARYING(32)`)
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER COLUMN "note" TYPE CHARACTER VARYING(128)`)
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER COLUMN "note" TYPE CHARACTER VARYING(16)`)
}

// TestContractTableAutoLayoutPlans runs plans of a table that leaves its layout to Redshift: a distribution changed outside
// Terraform shows up and is set back to AUTO, and dropping a column Redshift chose as distribution and sort key
// detours through EVEN and no sort key before the drop.
func TestContractTableAutoLayoutPlans(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	fake := fakeState[*tableFake](c, "table")
	delete(fake.tables, "serving.events")
	fake.templates["serving.events"] = tableFakeTable{owner: "admin", distStyle: "9", autoDistStyle: "11", autoSortKey: true, columns: []tableFakeColumn{
		{name: "id", dataType: "bigint", encoding: "az64"},
		{name: "note", dataType: "character varying(32)", encoding: "lzo"},
	}}
	table := func() *tableFakeTable { return fake.tables["serving.events"] }

	var detour int

	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: tableAutoConfig(tableNoteColumn("varchar(32)")), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckNoResourceAttr("redshift_table.events", "distribution.style"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_distribution.style", "EVEN"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_distribution.auto", "true"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_sort_key.style", "NONE"),
				testresource.TestCheckResourceAttr("redshift_table.events", "effective_sort_key.auto", "true"),
			)},
			{
				Config: tableAutoConfig(tableNoteColumn("varchar(32)")), PreConfig: func() { table().distStyle = "0" }, ConfigPlanChecks: tableExpectUpdate(),
				Check: testresource.TestCheckNoResourceAttr("redshift_table.events", "distribution.style"),
			},
			{Config: tableAutoConfig(""), ConfigPlanChecks: tableExpectUpdate(), PreConfig: func() {
				detour = len(c.writes)
				table().autoDistStyle = "12"
				table().columns[1].distKey, table().columns[1].sortKey = true, 1
			}},
		},
	})
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO`, "drift is planned back to AUTO")
	assert.Equal(t, []string{
		`ALTER TABLE "serving"."events" ALTER SORTKEY NONE`, `ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN`, `ALTER TABLE "serving"."events" DROP COLUMN "note"`,
		`ALTER TABLE "serving"."events" ALTER SORTKEY AUTO`, `ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO`,
	}, slices.DeleteFunc(slices.Clone(c.writes[detour:]), func(sql string) bool { return strings.HasPrefix(sql, "DROP TABLE") }))
}

// TestContractTableImportPlan imports the existing serving.events table: state then holds catalog spellings and no backup,
// so the first plan updates in place only to record the configured spellings, without any DDL, and the next plan
// is empty.
func TestContractTableImportPlan(t *testing.T) {
	t.Parallel()

	c := fullCatalog()
	config := tableConfig(tableIDColumn+tableLabelColumn, "") + `
import {
  to = redshift_table.events
  id = jsonencode({ workgroup_name = "warehouse", database = "admin", schema = "serving", name = "events" })
}
`
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: config, ConfigPlanChecks: tableExpectUpdate()},
		},
	})
	c.mu.Lock()
	defer c.mu.Unlock()

	assert.Equal(t, []string{`DROP TABLE "serving"."events"`}, c.writes, "only the final destroy writes")
}
