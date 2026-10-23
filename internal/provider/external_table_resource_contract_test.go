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

// TestContractExternalTablePropertiesPlan runs Terraform so the plan sees the catalog properties recorded in private state:
// adding a create-only property the catalog lacks must plan a replacement, not an update that fails, while an
// imported table keeps the catalog's matching properties in place.
func TestContractExternalTablePropertiesPlan(t *testing.T) {
	t.Parallel()
	const address = "redshift_external_table.events"
	gzip := `table_properties = { compression_type = "gzip" }`
	providers := func(c *catalog) map[string]func() (tfprotov6.ProviderServer, error) {
		return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})}
	}
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	parameter := func(c *catalog, key, value string) testresource.TestCheckFunc {
		return func(*terraform.State) error {
			// fakeState locks the catalog itself, so the table is looked up before reading it under the lock.
			table := externalTableFakeOf(c)
			c.mu.Lock()
			defer c.mu.Unlock()
			assert.Equal(t, value, table.parameters[key])
			return nil
		}
	}
	t.Run("created without properties", func(t *testing.T) {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableProviderConfig("")},
				{Config: externalTableProviderConfig(gzip), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate), Check: parameter(c, "compression_type", "gzip")},
			},
		})
	})
	for name, test := range map[string]struct {
		properties string
		action     plancheck.ResourceActionType
		key, value string
	}{
		// Only the catalog values recorded at import tell the matching compression_type apart from an addition.
		"imported, settable change": {`table_properties = { compression_type = "gzip", numRows = "7" }`, plancheck.ResourceActionUpdate, "numRows", "7"},
		"imported, create-only":     {`table_properties = { compression_type = "snappy" }`, plancheck.ResourceActionDestroyBeforeCreate, "compression_type", "snappy"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			externalTableFakeOf(c).parameters["compression_type"] = "gzip"
			t.Parallel()
			testresource.UnitTest(t, testresource.TestCase{
				ProtoV6ProviderFactories: providers(c),
				Steps: []testresource.TestStep{
					{
						Config: externalTableProviderConfig(""), ResourceName: address, ImportState: true, ImportStatePersist: true,
						ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
					},
					{Config: externalTableProviderConfig(""), PlanOnly: true},
					{Config: externalTableProviderConfig(test.properties), ConfigPlanChecks: expect(test.action), Check: parameter(c, test.key, test.value)},
				},
			})
		})
	}
}

// TestContractExternalTableColumnOrderPlan runs Terraform over block edits: a table that maps columns by name adds and
// drops columns anywhere in place and keeps the configured order in state although the catalog appends, while a
// positional table, including an ORC table whose catalog says position, replaces for the same edits. Switching to
// position mapping stays in place only while the blocks follow the catalog order.
func TestContractExternalTableColumnOrderPlan(t *testing.T) {
	t.Parallel()
	const address = "redshift_external_table.events"
	providers := func(c *catalog) map[string]func() (tfprotov6.ProviderServer, error) {
		return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})}
	}
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	// catalogColumns checks the physical column order and the statements since the previous check.
	catalogColumns := func(c *catalog, writes []string, names ...string) testresource.TestCheckFunc {
		return func(*terraform.State) error {
			table := externalTableFakeOf(c)
			c.mu.Lock()
			defer c.mu.Unlock()
			var physical []string
			for _, column := range table.columns {
				physical = append(physical, column.name)
			}
			assert.Equal(t, names, physical)
			assert.Equal(t, writes, c.writes)
			c.writes = nil
			return nil
		}
	}
	empty := func() *catalog {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		return c
	}
	alter := `ALTER TABLE "example_external"."events" `
	for _, format := range []string{"ORC", "PARQUET"} {
		t.Run(format+" maps by name", func(t *testing.T) {
			c := empty()
			created := externalTableOrderConfig(format, "", "id", "INTEGER", "label", "VARCHAR(64)")
			inserted := externalTableOrderConfig(format, "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)")
			reordered := externalTableOrderConfig(format, "", "label", "VARCHAR(64)", "amount", "DECIMAL(8,2)", "id", "INTEGER")
			dropped := externalTableOrderConfig(format, "", "label", "VARCHAR(64)", "id", "INTEGER")
			t.Parallel()
			testresource.UnitTest(t, testresource.TestCase{
				ProtoV6ProviderFactories: providers(c),
				Steps: []testresource.TestStep{
					{Config: created, Check: catalogColumns(c, []string{`CREATE EXTERNAL TABLE "example_external"."events" ("id" INTEGER, "label" VARCHAR(64)) STORED AS ` + format + ` LOCATION 's3://example-bucket/events/'`}, "id", "label")},
					{Config: inserted, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.ComposeTestCheckFunc(
						catalogColumns(c, []string{alter + `ADD COLUMN "amount" DECIMAL(8, 2)`}, "id", "label", "amount"),
						testresource.TestCheckResourceAttr(address, "column.1.name", "amount"),
					)},
					{Config: reordered, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.ComposeTestCheckFunc(
						catalogColumns(c, nil, "id", "label", "amount"),
						testresource.TestCheckResourceAttr(address, "column.0.name", "label"),
					)},
					{Config: dropped, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: catalogColumns(c, []string{alter + `DROP COLUMN "amount"`}, "id", "label")},
				},
			})
		})
	}
	for _, inOrder := range []bool{false, true} {
		t.Run(fmt.Sprintf("parquet to textfile in catalog order %t", inOrder), func(t *testing.T) {
			c := empty()
			switched := externalTableOrderConfig("TEXTFILE", "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)")
			action := plancheck.ResourceActionDestroyBeforeCreate
			if inOrder {
				switched = externalTableOrderConfig("TEXTFILE", "", "id", "INTEGER", "label", "VARCHAR(64)", "amount", "DECIMAL(8,2)")
				action = plancheck.ResourceActionUpdate
			}
			t.Parallel()
			testresource.UnitTest(t, testresource.TestCase{ProtoV6ProviderFactories: providers(c), Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("PARQUET", "", "id", "INTEGER", "label", "VARCHAR(64)")},
				{Config: externalTableOrderConfig("PARQUET", "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)")},
				{Config: switched, ConfigPlanChecks: expect(action)},
			}})
		})
	}
	t.Run("textfile maps by position", func(t *testing.T) {
		inserted := externalTableOrderConfig("TEXTFILE", "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)")
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(empty()),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("TEXTFILE", "", "id", "INTEGER", "label", "VARCHAR(64)")},
				{Config: inserted, ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
			},
		})
	})
	t.Run("imported orc by position", func(t *testing.T) {
		c := fullCatalog()
		table := externalTableFakeOf(c)
		table.inputFormat, table.serde, table.partitionKeys, table.partitions = externalTableFileFormats["ORC"], externalTableImpliedSerdes["ORC"], nil, nil
		table.serdeParameters, table.parameters = map[string]string{}, map[string]string{"EXTERNAL": "TRUE", "orc.schema.resolution": "position"}
		imported := externalTableOrderConfig("ORC", "", "id", "INTEGER", "label", "VARCHAR(64)")
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{
					Config: imported, ResourceName: address, ImportState: true, ImportStatePersist: true,
					ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
				},
				{Config: imported, PlanOnly: true},
				{Config: externalTableOrderConfig("ORC", "", "label", "VARCHAR(64)", "id", "INTEGER"), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
			},
		})
	})
	position := `table_properties = { "orc.schema.resolution" = "position" }`
	create := `CREATE EXTERNAL TABLE "example_external"."events" ("id" INTEGER, "label" VARCHAR(64)) STORED AS ORC LOCATION 's3://example-bucket/events/'`
	t.Run("orc switching to position out of catalog order", func(t *testing.T) {
		c := empty()
		switched := externalTableOrderConfig("ORC", position, "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)")
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "label", "VARCHAR(64)"), Check: catalogColumns(c, []string{create}, "id", "label")},
				{Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)"), Check: catalogColumns(c, []string{alter + `ADD COLUMN "amount" DECIMAL(8, 2)`}, "id", "label", "amount")},
				{Config: switched, ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate), Check: catalogColumns(c, []string{
					`DROP TABLE "example_external"."events"`,
					`CREATE EXTERNAL TABLE "example_external"."events" ("id" INTEGER, "amount" DECIMAL(8, 2), "label" VARCHAR(64)) STORED AS ORC LOCATION 's3://example-bucket/events/' TABLE PROPERTIES ('orc.schema.resolution' = 'position')`,
				}, "id", "amount", "label")},
			},
		})
	})
	t.Run("orc switching to position in catalog order", func(t *testing.T) {
		c := empty()
		switched := externalTableOrderConfig("ORC", position, "id", "INTEGER", "label", "VARCHAR(64)", "amount", "DECIMAL(8,2)")
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "label", "VARCHAR(64)"), Check: catalogColumns(c, []string{create}, "id", "label")},
				{Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)"), Check: catalogColumns(c, []string{alter + `ADD COLUMN "amount" DECIMAL(8, 2)`}, "id", "label", "amount")},
				{Config: switched, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: catalogColumns(c, []string{alter + `SET TABLE PROPERTIES ('orc.schema.resolution' = 'position')`}, "id", "label", "amount")},
			},
		})
	})
	// The catalog's mapping still decides once table_properties is configured without orc.schema.resolution.
	for name, edited := range map[string][]string{
		"reorder": {"label", "VARCHAR(64)", "id", "INTEGER"},
		"insert":  {"id", "INTEGER", "amount", "DECIMAL(8,2)", "label", "VARCHAR(64)"},
	} {
		t.Run("imported orc by position with managed properties, "+name, func(t *testing.T) {
			c := fullCatalog()
			table := externalTableFakeOf(c)
			table.inputFormat, table.serde, table.partitionKeys, table.partitions = externalTableFileFormats["ORC"], externalTableImpliedSerdes["ORC"], nil, nil
			table.serdeParameters, table.parameters = map[string]string{}, map[string]string{"EXTERNAL": "TRUE", "orc.schema.resolution": "position"}
			managed := `table_properties = { numRows = "7" }`
			t.Parallel()
			testresource.UnitTest(t, testresource.TestCase{
				ProtoV6ProviderFactories: providers(c),
				Steps: []testresource.TestStep{
					{
						Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "label", "VARCHAR(64)"), ResourceName: address, ImportState: true, ImportStatePersist: true,
						ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
					},
					{Config: externalTableOrderConfig("ORC", managed, "id", "INTEGER", "label", "VARCHAR(64)"), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate)},
					{Config: externalTableOrderConfig("ORC", managed, edited...), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
				},
			})
		})
	}
	t.Run("orc set to position outside terraform", func(t *testing.T) {
		c := empty()
		t.Parallel()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "INTEGER", "label", "VARCHAR(64)")},
				{
					PreConfig: func() {
						table := externalTableFakeOf(c)
						c.mu.Lock()
						defer c.mu.Unlock()
						table.parameters["orc.schema.resolution"] = "position"
					},
					Config: externalTableOrderConfig("ORC", "", "label", "VARCHAR(64)", "id", "INTEGER"), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
		})
	})
}
