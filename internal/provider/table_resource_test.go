package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_table", map[string]replaceRule{
	"database":     replaceAlways,
	"schema":       replaceAlways,
	"name":         replaceAlways,
	"owner":        replaceNever,
	"column":       replaceConditional("TestTableConditionalReplacement"),
	"primary_key":  replaceConditional("TestTableConditionalReplacement"),
	"unique":       replaceNever,
	"foreign_key":  replaceNever,
	"distribution": replaceConditional("TestTableConditionalReplacement"),
	"sort_key":     replaceConditional("TestTableConditionalReplacement"),
	"backup":       replaceConditional("TestTableConditionalReplacement"),
})

var _ = registerLifecycleCase(lifecycleCase{
	name: "table", new: newTableResource, model: tableEventsModel(),
	absent: func(c *catalog) { delete(fakeState[*tableFake](c, "table").tables, "serving.events") },
})

var _ = registerValidateConfigCase("table", validateConfigCase{
	new:   newTableResource,
	valid: tableEventsModel(),
	invalid: tableWith(tableEventsModel(), func(m *tableModel) {
		m.Distribution = tableTestDistribution("", "missing")
	}),
	unknown: tableWith(tableEventsModel(), func(m *tableModel) { m.Name = types.StringUnknown() }),
})

// tableTestColumn builds a column with typed nulls for every option.
func tableTestColumn(name, dataType string, options ...func(*tableColumnModel)) tableColumnModel {
	column := tableColumnModel{
		Name: types.StringValue(name), Type: types.StringValue(dataType), Encoding: types.StringNull(), Nullable: types.BoolNull(),
		Default: types.StringNull(), Identity: types.ObjectNull(tableIdentityAttributeTypes),
	}
	for _, option := range options {
		option(&column)
	}
	return column
}

// tableEncoded sets a column encoding.
func tableEncoded(encoding string) func(*tableColumnModel) {
	return func(c *tableColumnModel) { c.Encoding = types.StringValue(encoding) }
}

// tableNullable sets a column's nullability.
func tableNullable(nullable bool) func(*tableColumnModel) {
	return func(c *tableColumnModel) { c.Nullable = types.BoolValue(nullable) }
}

// tableDefault sets a column default.
func tableDefault(text string) func(*tableColumnModel) {
	return func(c *tableColumnModel) { c.Default = types.StringValue(text) }
}

// tableIdentity makes a column an identity column.
func tableIdentity(seed, step int64, byDefault bool) func(*tableColumnModel) {
	return func(c *tableColumnModel) {
		c.Identity = types.ObjectValueMust(tableIdentityAttributeTypes, map[string]attr.Value{
			"seed": types.Int64Value(seed), "step": types.Int64Value(step), "generated_by_default": types.BoolValue(byDefault),
		})
	}
}

// tableTestList builds a list of names.
func tableTestList(names ...string) types.List {
	return tableStringListValue(names)
}

// tableTestColumns builds the column block list.
func tableTestColumns(columns ...tableColumnModel) types.List {
	if columns == nil {
		columns = []tableColumnModel{}
	}
	list, diagnostics := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: tableColumnAttributeTypes}, columns)
	if diagnostics.HasError() {
		panic(fmt.Sprint(diagnostics))
	}
	return list
}

// tableTestKey builds a primary_key or unique block.
func tableTestKey(names ...string) types.Object {
	return types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": tableTestList(names...)})
}

// tableTestUnique builds the unique blocks.
func tableTestUnique(lists ...[]string) types.Set {
	elements := make([]attr.Value, len(lists))
	for i, names := range lists {
		elements[i] = tableTestKey(names...)
	}
	return types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, elements)
}

// tableTestReferences builds a references block.
func tableTestReferences(schemaName, table types.String, columns types.List) types.Object {
	return types.ObjectValueMust(tableReferencesAttributeTypes, map[string]attr.Value{"schema": schemaName, "table": table, "columns": columns})
}

// tableTestForeignKey builds one foreign_key block.
func tableTestForeignKey(columns []string, refSchema, refTable string, refColumns ...string) attr.Value {
	return types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
		"columns":    tableTestList(columns...),
		"references": tableTestReferences(types.StringValue(refSchema), types.StringValue(refTable), tableTestList(refColumns...)),
	})
}

// tableTestForeignKeys builds the foreign_key blocks.
func tableTestForeignKeys(keys ...attr.Value) types.Set {
	return types.SetValueMust(types.ObjectType{AttrTypes: tableForeignKeyAttributeTypes}, keys)
}

// tableTestDistribution builds a distribution block; empty strings are omitted attributes.
func tableTestDistribution(style, key string) types.Object {
	return types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": tableOptionalString(style), "key": tableOptionalString(key)})
}

// tableTestSortKey builds a sort_key block; no columns leaves them omitted.
func tableTestSortKey(style string, columns ...string) types.Object {
	list := types.ListNull(types.StringType)
	if len(columns) > 0 {
		list = tableTestList(columns...)
	}
	return types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": tableOptionalString(style), "columns": list})
}

// tableAuto omits both layout blocks and records layouts Redshift chose, as a read of an AUTO table reports them.
func tableAuto(distKey string, sortKey ...string) func(*tableModel) {
	return func(m *tableModel) {
		distStyle := "EVEN"
		if distKey != "" {
			distStyle = "KEY"
		}
		sortStyle := "NONE"
		if len(sortKey) > 0 {
			sortStyle = "COMPOUND"
		}
		m.Distribution, m.SortKey = types.ObjectNull(tableDistributionAttributeTypes), types.ObjectNull(tableSortKeyAttributeTypes)
		m.EffectiveDistribution = tableEffectiveDistributionValue(distStyle, distKey, true)
		m.EffectiveSortKey = tableEffectiveSortKeyValue(sortStyle, sortKey, true)
	}
}

// tableWith returns a copy of model changed by change.
func tableWith(model tableModel, change func(*tableModel)) tableModel {
	change(&model)
	return model
}

// tableEventsModel is the configuration matching the fake's representative serving.events table, with the
// effective layout a read reports.
func tableEventsModel() tableModel {
	model := tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events"))
	model.Owner = types.StringValue("admin")
	model.Column = tableTestColumns(
		tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
		tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
	)
	model.PrimaryKey = tableTestKey("id")
	model.Distribution = tableTestDistribution("KEY", "id")
	model.SortKey = tableTestSortKey("COMPOUND", "id")
	model.Backup = types.StringValue("YES")
	model.EffectiveDistribution = tableEffectiveDistributionValue("KEY", "id", false)
	model.EffectiveSortKey = tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, false)
	return model
}

// tableSpecMust validates a model for tests.
func tableSpecMust(t *testing.T, model tableModel) tableSpec {
	t.Helper()
	spec, err := tableSpecOf(model)
	require.NoError(t, err)
	return spec
}

// TestTableConditionalReplacement covers both branches of every conditionally replacing attribute and block: the
// changes ALTER TABLE makes in place and those it cannot make. Columns match by name, so reordering and inserting
// columns update in place.
func TestTableConditionalReplacement(t *testing.T) {
	base := tableEventsModel()
	withColumns := func(columns ...tableColumnModel) tableModel {
		return tableWith(base, func(m *tableModel) { m.Column = tableTestColumns(columns...) })
	}
	id := tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false))
	label := tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'"))
	note := func(options ...func(*tableColumnModel)) tableColumnModel {
		return tableTestColumn("note", "varchar(32)", append([]func(*tableColumnModel){tableEncoded("LZO"), tableNullable(true)}, options...)...)
	}
	withNote := withColumns(id, label, note())
	interleaved := func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }
	autoSort := func(m *tableModel) { m.SortKey = types.ObjectNull(tableSortKeyAttributeTypes) }
	autoDistribution := func(m *tableModel) { m.Distribution = types.ObjectNull(tableDistributionAttributeTypes) }
	wide := func(count int) tableModel {
		columns := []tableColumnModel{id, label}
		for i := range count {
			columns = append(columns, tableTestColumn(fmt.Sprintf("c%d", i), "int"))
		}
		return withColumns(columns...)
	}
	for _, test := range []struct {
		name      string
		prev      tableModel
		plan      tableModel
		attribute string
		replace   bool
	}{
		{"append column", base, withNote, "column", false},
		{"drop column", withNote, base, "column", false},
		{"insert column before existing", base, withColumns(id, note(), label), "column", false},
		{"reorder columns", withNote, withColumns(note(), label, id), "column", false},
		{"drop middle column", withColumns(id, note(), label), withColumns(id, label), "column", false},
		{"rename column", withNote, withColumns(id, label, tableTestColumn("remark", "varchar(32)", tableEncoded("LZO"), tableNullable(true))), "column", false},
		{"column limit", wide(tableMaxColumns - 3), tableWith(wide(tableMaxColumns-3), func(m *tableModel) {
			m.Column = tableTestColumns(append(tableTestColumnsOf(t, m.Column), tableTestColumn("extra", "int"))...)
		}), "column", false},
		{"column limit exceeded", wide(tableMaxColumns - 2), tableWith(wide(tableMaxColumns-3), func(m *tableModel) {
			m.Column = tableTestColumns(append(tableTestColumnsOf(t, m.Column)[1:], tableTestColumn("extra", "int"))...)
			m.PrimaryKey, m.Distribution, m.SortKey = types.ObjectNull(tableKeyAttributeTypes), types.ObjectNull(tableDistributionAttributeTypes), types.ObjectNull(tableSortKeyAttributeTypes)
		}), "column", true},
		{"append identity column", base, withColumns(id, label, tableTestColumn("seq", "integer", tableIdentity(1, 1, true))), "column", true},
		{"append NOT NULL column without default", base, withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false))), "column", true},
		{"append NOT NULL column with default", base, withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false), tableDefault("'xx'"))), "column", false},
		{"new primary key column without default", base, tableWith(withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false))), func(m *tableModel) {
			m.PrimaryKey = tableTestKey("id", "code")
		}), "column", true},
		{"widen varchar", withNote, withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), "column", false},
		{"widen varchar alias", withNote, withColumns(id, label, tableTestColumn("note", "character varying(33)", tableEncoded("LZO"), tableNullable(true))), "column", false},
		{"same type alias", withNote, withColumns(id, label, tableTestColumn("note", "nvarchar(32)", tableEncoded("LZO"), tableNullable(true))), "column", false},
		{"widen varbyte", withColumns(id, label, tableTestColumn("blob", "varbyte(16)", tableEncoded("LZO"), tableNullable(true))),
			withColumns(id, label, tableTestColumn("blob", "varbinary(32)", tableEncoded("LZO"), tableNullable(true))), "column", false},
		{"varbyte to varchar", withColumns(id, label, tableTestColumn("blob", "varbyte(16)", tableEncoded("LZO"), tableNullable(true))),
			withColumns(id, label, tableTestColumn("blob", "varchar(32)", tableEncoded("LZO"), tableNullable(true))), "column", true},
		{"narrow varchar", withNote, withColumns(id, label, tableTestColumn("note", "varchar(16)", tableEncoded("LZO"), tableNullable(true))), "column", true},
		{"change type", withNote, withColumns(id, label, tableTestColumn("note", "integer", tableEncoded("LZO"), tableNullable(true))), "column", true},
		{"widen column with default", base, withColumns(id, tableTestColumn("label", "varchar(128)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'"))), "column", true},
		{"widen constrained column", tableWith(withNote, func(m *tableModel) { m.Unique = tableTestUnique([]string{"note"}) }),
			tableWith(withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), func(m *tableModel) { m.Unique = tableTestUnique([]string{"note"}) }), "column", true},
		{"widen foreign key column", tableWith(withNote, func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"note"}, "serving", "notes", "note"))
		}), tableWith(withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"note"}, "serving", "notes", "note"))
		}), "column", true},
		{"widen bytedict column", withColumns(id, label, note(tableEncoded("BYTEDICT"))), withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("BYTEDICT"), tableNullable(true))), "column", true},
		{"change encoding", withNote, withColumns(id, label, note(tableEncoded("ZSTD"))), "column", false},
		{"change encoding with interleaved sort key", tableWith(withNote, interleaved), tableWith(withColumns(id, label, note(tableEncoded("ZSTD"))), interleaved), "column", true},
		{"change nullability", withNote, withColumns(id, label, note(tableNullable(false))), "column", true},
		{"add default", withNote, withColumns(id, label, note(tableDefault("'x'"))), "column", true},
		{"default respelled", base, withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'::character varying"))), "column", false},
		{"change default", base, withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'other'"))), "column", true},
		{"change identity", base, withColumns(tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 2, false)), label), "column", true},
		{"primary key on not null column", tableWith(withColumns(id, label, note(tableNullable(false))), func(m *tableModel) { m.PrimaryKey = types.ObjectNull(tableKeyAttributeTypes) }),
			withColumns(id, label, note(tableNullable(false))), "primary_key", false},
		{"primary key on nullable column", base, tableWith(withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(false), tableDefault("'none'"))),
			func(m *tableModel) { m.PrimaryKey = tableTestKey("label") }), "primary_key", true},
		{"drop primary key", base, tableWith(base, func(m *tableModel) { m.PrimaryKey = types.ObjectNull(tableKeyAttributeTypes) }), "primary_key", false},
		{"distribution even", base, tableWith(base, func(m *tableModel) { m.Distribution = tableTestDistribution("EVEN", "") }), "distribution", false},
		{"distribution with interleaved sort key", tableWith(base, interleaved), tableWith(base, func(m *tableModel) {
			interleaved(m)
			m.Distribution = tableTestDistribution("ALL", "")
		}), "distribution", true},
		{"distribution key", withNote, tableWith(withNote, func(m *tableModel) { m.Distribution = tableTestDistribution("", "note") }), "distribution", false},
		{"distribution key with interleaved sort key", tableWith(withNote, interleaved), tableWith(withNote, func(m *tableModel) {
			interleaved(m)
			m.Distribution = tableTestDistribution("KEY", "note")
		}), "distribution", true},
		{"distribution block for auto", tableWith(base, autoDistribution), tableWith(base, func(m *tableModel) { m.Distribution = tableTestDistribution("AUTO", "") }), "distribution", false},
		{"leave interleaved sort key", tableWith(base, interleaved), base, "sort_key", false},
		{"sort key auto", base, tableWith(base, autoSort), "sort_key", false},
		{"sort key none", base, tableWith(base, func(m *tableModel) { m.SortKey = tableTestSortKey("NONE") }), "sort_key", false},
		{"interleaved sort key", base, tableWith(base, interleaved), "sort_key", true},
		{"interleaved sort key to auto", tableWith(base, interleaved), tableWith(base, autoSort), "sort_key", false},
		{"auto sort key without the dropped key column", tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("", "note") }), tableWith(base, autoSort), "sort_key", false},
		{"drop automatic distribution key with interleaved sort key", tableWith(withNote, func(m *tableModel) {
			tableAuto("note")(m)
			interleaved(m)
		}), tableWith(base, func(m *tableModel) {
			autoDistribution(m)
			interleaved(m)
		}), "column", true},
		{"drop column with interleaved sort key", tableWith(withNote, func(m *tableModel) {
			tableAuto("id")(m)
			interleaved(m)
		}), tableWith(base, func(m *tableModel) {
			autoDistribution(m)
			interleaved(m)
		}), "column", false},
		{"auto distribution without the dropped key column", tableWith(withNote, func(m *tableModel) { m.Distribution = tableTestDistribution("", "note") }), tableWith(base, autoDistribution), "distribution", false},
		{"compound sort key columns", withNote, tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("", "id", "note") }), "sort_key", false},
		{"interleaved sort key columns", tableWith(withNote, interleaved), tableWith(withNote, func(m *tableModel) {
			m.SortKey = tableTestSortKey("INTERLEAVED", "id", "note")
		}), "sort_key", true},
		{"backup after import", tableWith(base, func(m *tableModel) { m.Backup = types.StringNull() }), tableWith(base, func(m *tableModel) { m.Backup = types.StringValue("NO") }), "backup", false},
		{"backup change", base, tableWith(base, func(m *tableModel) { m.Backup = types.StringValue("NO") }), "backup", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reasons := tableReplacements(tableSpecMust(t, test.prev), tableSpecMust(t, test.plan))
			assert.Equal(t, test.replace, reasons[test.attribute] != "", "%v", reasons)
			// The schema's plan modifier reaches the same decision from Terraform state and plan.
			r := newTableResource()
			state := testState(t, r, test.prev)
			planned := testState(t, r, test.plan)
			assert.Equal(t, test.replace, tableRequiresReplace(context.Background(), tfsdk.Config(planned), state, tfsdk.Plan(planned), test.attribute))
		})
	}
	t.Run("undecodable state replaces", func(t *testing.T) {
		r := newTableResource()
		invalid := tableWith(base, func(m *tableModel) { m.Distribution = tableTestDistribution("", "missing") })
		valid := testState(t, r, base)
		assert.True(t, tableRequiresReplace(context.Background(), tfsdk.Config(valid), testState(t, r, invalid), tfsdk.Plan(valid), "distribution"))
		assert.True(t, tableRequiresReplace(context.Background(), tfsdk.Config(testState(t, r, invalid)), valid, tfsdk.Plan(testState(t, r, invalid)), "distribution"))
		assert.True(t, tableRequiresReplace(context.Background(), tfsdk.Config(emptyState(t, newSchemaResource())), valid, tfsdk.Plan(valid), "distribution"))
	})
	// The plan may still hold an unknown style that tableDerivedStyle fills in later, so an unknown style decides
	// nothing; only a configured one that resolves during apply, possibly to INTERLEAVED, replaces.
	t.Run("unknown configured style replaces", func(t *testing.T) {
		r := newTableResource()
		state := testState(t, r, base)
		pending := func(m *tableModel) {
			m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "columns": tableStringListValue([]string{"id"})})
		}
		planned := testState(t, r, tableWith(base, pending))
		assert.False(t, tableRequiresReplace(context.Background(), tfsdk.Config(state), state, tfsdk.Plan(planned), "sort_key"), "an unknown planned style alone is not configuration")
		assert.True(t, tableRequiresReplace(context.Background(), tfsdk.Config(planned), state, tfsdk.Plan(planned), "sort_key"))
		distribution := testState(t, r, tableWith(base, func(m *tableModel) {
			m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "key": types.StringNull()})
		}))
		assert.True(t, tableRequiresReplace(context.Background(), tfsdk.Config(distribution), state, tfsdk.Plan(distribution), "distribution"))
	})
}

// TestTableColumnOrder orders catalog columns by the prior names, appends unknown ones in physical order, and drops
// names the catalog no longer has.
func TestTableColumnOrder(t *testing.T) {
	assert.Equal(t, []string{"id", "note", "label"}, tableColumnOrder([]string{"id", "note", "label"}, []string{"id", "label", "note"}), "a column inserted in the middle keeps its place")
	assert.Equal(t, []string{"label", "id", "extra", "other"}, tableColumnOrder([]string{"label", "gone", "id"}, []string{"id", "extra", "label", "other"}))
	assert.Equal(t, []string{"id", "label"}, tableColumnOrder(nil, []string{"id", "label"}), "import uses physical order")
	assert.Empty(t, tableColumnOrder([]string{"id"}, nil))

	c := fullCatalog()
	tableFakeNote(fakeState[*tableFake](c, "table").tables["serving.events"])
	r := &tableResource{testResourceClient(c)}
	data := tableWith(tableEventsModel(), func(m *tableModel) {
		columns := tableTestColumnsOf(t, m.Column)
		m.Column = tableTestColumns(columns[1], tableTestColumn("note", "varchar(32)"), columns[0])
	})
	found, _, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	var names []string
	for _, column := range tableTestColumnsOf(t, data.Column) {
		names = append(names, column.Name.ValueString())
	}
	assert.Equal(t, []string{"label", "note", "id"}, names, "read keeps the prior order")
}

// TestTableAlterCoverage keeps an update step for every in-place attribute and block.
func TestTableAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newTableResource(), tableAlterSteps(tablePhaseDropConstraints))
}

// TestTableSpecValidation rejects definitions Redshift would refuse before any SQL runs.
func TestTableSpecValidation(t *testing.T) {
	base := tableEventsModel()
	columns := func(columns ...tableColumnModel) func(*tableModel) {
		return func(m *tableModel) { m.Column = tableTestColumns(columns...) }
	}
	noKeys := func(m *tableModel) {
		m.PrimaryKey, m.Distribution, m.SortKey = types.ObjectNull(tableKeyAttributeTypes), types.ObjectNull(tableDistributionAttributeTypes), types.ObjectNull(tableSortKeyAttributeTypes)
	}
	foreignKey := func(columns types.List, schemaName, table types.String, refColumns types.List) func(*tableModel) {
		return func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
				"columns": columns, "references": tableTestReferences(schemaName, table, refColumns),
			}))
		}
	}
	for _, test := range []struct {
		name    string
		change  func(*tableModel)
		message string
	}{
		{"empty schema", func(m *tableModel) { m.Schema = types.StringValue("") }, "schema and name"},
		{"unknown name", func(m *tableModel) { m.Name = types.StringUnknown() }, "name must be known"},
		{"unknown columns", func(m *tableModel) {
			m.Column = types.ListUnknown(types.ObjectType{AttrTypes: tableColumnAttributeTypes})
		}, "column must be known"},
		{"no columns", columns(), "between 1 and 1600"},
		{"duplicate column", columns(tableTestColumn("a", "int"), tableTestColumn("a", "int")), "declared twice"},
		{"empty column name", columns(tableTestColumn("", "int")), "name must be nonempty"},
		{"unknown type", columns(tableTestColumn("a", "uuid")), "unsupported Redshift data type"},
		{"bad encoding", columns(tableTestColumn("a", "int", tableEncoded("SNAPPY"))), "encoding"},
		{"statement in default", columns(tableTestColumn("a", "int", tableDefault("1; DROP TABLE x"))), "single statement"},
		{"unknown default", columns(tableTestColumn("a", "int", func(c *tableColumnModel) { c.Default = types.StringUnknown() })), "default must be known"},
		{"identity text", columns(tableTestColumn("a", "varchar", tableIdentity(1, 1, false))), "INTEGER or BIGINT"},
		{"identity zero step", columns(tableTestColumn("a", "int", tableIdentity(1, 0, false))), "nonzero step"},
		{"identity with default", columns(tableTestColumn("a", "int", tableIdentity(1, 1, false), tableDefault("1"))), "cannot also have a default"},
		{"nullable identity", columns(tableTestColumn("a", "int", tableIdentity(1, 1, false), tableNullable(true))), "always NOT NULL"},
		{"unknown primary key column", func(m *tableModel) { m.PrimaryKey = tableTestKey("missing") }, "primary_key.columns names column"},
		{"repeated primary key column", func(m *tableModel) { m.PrimaryKey = tableTestKey("id", "id") }, "distinct"},
		{"nullable primary key", func(m *tableModel) { m.PrimaryKey = tableTestKey("label") }, "cannot be nullable"},
		{"unknown unique column", func(m *tableModel) { m.Unique = tableTestUnique([]string{"missing"}) }, "unique.columns names column"},
		{"unknown foreign key column", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"missing"}, "serving", "accounts", "id"))
		}, "foreign_key.columns names column"},
		{"foreign key arity", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "accounts", "id", "other"))
		}, "as many references columns"},
		{"foreign key without table", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "", "id"))
		}, "references schema and table"},
		{"foreign key without references", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
				"columns": tableTestList("id"), "references": types.ObjectNull(tableReferencesAttributeTypes),
			}))
		}, "needs a references block"},
		{"key without distribution key", func(m *tableModel) { m.Distribution = tableTestDistribution("KEY", "") }, "needs a key"},
		{"distribution key without key style", func(m *tableModel) { m.Distribution = tableTestDistribution("EVEN", "id") }, "requires style KEY"},
		{"bad distribution style", func(m *tableModel) { m.Distribution = tableTestDistribution("RANDOM", "") }, "distribution.style"},
		{"unknown distribution", func(m *tableModel) { m.Distribution = types.ObjectUnknown(tableDistributionAttributeTypes) }, "distribution must be known"},
		{"auto with sort key", func(m *tableModel) { m.SortKey = tableTestSortKey("AUTO", "id") }, "remove sort_key.columns"},
		{"none with sort key", func(m *tableModel) { m.SortKey = tableTestSortKey("NONE", "id") }, "remove sort_key.columns"},
		{"compound without columns", func(m *tableModel) { m.SortKey = tableTestSortKey("COMPOUND") }, "needs sort_key.columns"},
		{"interleaved limit", func(m *tableModel) {
			var many []tableColumnModel
			var names []string
			for i := range 9 {
				name := fmt.Sprintf("c%d", i)
				many, names = append(many, tableTestColumn(name, "int")), append(names, name)
			}
			noKeys(m)
			m.Column, m.SortKey = tableTestColumns(many...), tableTestSortKey("INTERLEAVED", names...)
		}, "at most 8"},
		{"bad backup", func(m *tableModel) { m.Backup = types.StringValue("MAYBE") }, "backup"},
		{"uppercase schema", func(m *tableModel) { m.Schema = types.StringValue("Serving") }, "folds to lowercase"},
		{"uppercase name", func(m *tableModel) { m.Name = types.StringValue(`Odd"Events`) }, "folds to lowercase"},
		{"uppercase owner", func(m *tableModel) { m.Owner = types.StringValue("Admin") }, "folds to lowercase"},
		{"uppercase column", columns(tableTestColumn("Id", "int")), "folds to lowercase"},
		{"uppercase referenced table", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "Accounts", "id"))
		}, "folds to lowercase"},
		{"super distribution key", columns(tableTestColumn("id", "super"), tableTestColumn("label", "varchar")), "does not allow in a key"},
		{"geometry sort key", func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "bigint"), tableTestColumn("shape", "geometry"))
			m.SortKey = tableTestSortKey("", "id", "shape")
		}, "does not allow in a key"},
		{"unknown primary key", func(m *tableModel) { m.PrimaryKey = types.ObjectUnknown(tableKeyAttributeTypes) }, "primary_key must be known"},
		{"primary key without columns", func(m *tableModel) {
			m.PrimaryKey = types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": types.ListNull(types.StringType)})
		}, "needs columns"},
		{"identity without step", columns(tableTestColumn("a", "int", func(c *tableColumnModel) {
			c.Identity = types.ObjectValueMust(tableIdentityAttributeTypes, map[string]attr.Value{"seed": types.Int64Value(1), "step": types.Int64Null(), "generated_by_default": types.BoolValue(false)})
		})), "needs a seed and a step"},
		{"unknown primary key columns", func(m *tableModel) {
			m.PrimaryKey = types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})})
		}, "known names"},
		{"empty unique", func(m *tableModel) { m.Unique = tableTestUnique([]string{}) }, "at least one column"},
		{"unknown unique", func(m *tableModel) { m.Unique = types.SetUnknown(types.ObjectType{AttrTypes: tableKeyAttributeTypes}) }, "unique must be known"},
		{"unknown unique columns", func(m *tableModel) {
			m.Unique = types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{
				types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": types.ListUnknown(types.StringType)}),
			})
		}, "unique.columns must be known"},
		{"unknown foreign keys", func(m *tableModel) {
			m.ForeignKey = types.SetUnknown(types.ObjectType{AttrTypes: tableForeignKeyAttributeTypes})
		}, "foreign_key must be known"},
		{"unknown foreign key columns", foreignKey(types.ListUnknown(types.StringType), types.StringValue("s"), types.StringValue("t"), tableTestList("id")), "foreign_key.columns must be known"},
		{"unknown referenced table", foreignKey(tableTestList("id"), types.StringValue("s"), types.StringUnknown(), tableTestList("id")), "foreign_key.references.table must be known"},
		{"unknown references", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
				"columns": tableTestList("id"), "references": types.ObjectUnknown(tableReferencesAttributeTypes),
			}))
		}, "foreign_key.references must be known"},
		{"repeated referenced column", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id", "label"}, "s", "t", "x", "x"))
		}, "distinct"},
		{"unknown distribution key", func(m *tableModel) {
			m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "key": types.StringUnknown()})
		}, "distribution.key must be known"},
		{"unknown sort key columns", func(m *tableModel) {
			m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "columns": types.ListUnknown(types.StringType)})
		}, "sort_key.columns must be known"},
		{"unknown sort key", func(m *tableModel) { m.SortKey = types.ObjectUnknown(tableSortKeyAttributeTypes) }, "sort_key must be known"},
		{"bad sort key style", func(m *tableModel) { m.SortKey = tableTestSortKey("RANDOM", "id") }, "sort_key.style"},
		{"unknown sort key column", func(m *tableModel) { m.SortKey = tableTestSortKey("", "missing") }, "does not declare"},
		{"unknown column type", columns(tableTestColumn("a", "int", func(c *tableColumnModel) { c.Type = types.StringUnknown() })), "type must be known"},
		{"unknown identity", columns(tableTestColumn("a", "int", func(c *tableColumnModel) { c.Identity = types.ObjectUnknown(tableIdentityAttributeTypes) })), "identity must be known"},
		{"unknown identity seed", columns(tableTestColumn("a", "int", func(c *tableColumnModel) {
			c.Identity = types.ObjectValueMust(tableIdentityAttributeTypes, map[string]attr.Value{"seed": types.Int64Unknown(), "step": types.Int64Value(1), "generated_by_default": types.BoolValue(false)})
		})), "identity must be known"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := tableSpecOf(tableWith(base, test.change))
			require.ErrorContains(t, err, test.message)
		})
	}
	spec := tableSpecMust(t, tableWith(base, func(m *tableModel) {
		m.Distribution, m.SortKey, m.Owner = tableTestDistribution("", "id"), tableTestSortKey("", "id"), types.StringUnknown()
	}))
	assert.Equal(t, "KEY", string(spec.distStyle), "a distribution key implies KEY")
	assert.Equal(t, "COMPOUND", string(spec.sortStyle), "sort key columns imply COMPOUND")
	assert.Empty(t, spec.owner, "an unknown owner is left to the catalog")
	auto := tableSpecMust(t, tableWith(base, func(m *tableModel) {
		m.Distribution, m.SortKey = types.ObjectNull(tableDistributionAttributeTypes), types.ObjectNull(tableSortKeyAttributeTypes)
	}))
	assert.Equal(t, "AUTO", string(auto.distStyle), "an omitted distribution block is AUTO")
	assert.Equal(t, "AUTO", string(auto.sortStyle), "an omitted sort_key block is AUTO")
	assert.Equal(t, "id", auto.effectiveDistKey, "the key Redshift applies under AUTO comes from effective_distribution")
	assert.Equal(t, []string{"id"}, auto.effectiveSortKey)
	explicit := tableSpecMust(t, tableWith(base, func(m *tableModel) {
		m.Distribution, m.SortKey = tableTestDistribution("EVEN", ""), tableTestSortKey("NONE")
	}))
	assert.Empty(t, explicit.effectiveDistKey, "an explicit layout ignores the recorded effective one")
	assert.Empty(t, explicit.effectiveSortKey)
}

// TestTableCatalogParsing pins how catalog text becomes definitions, including quoted names.
func TestTableCatalogParsing(t *testing.T) {
	columns, refs, err := tableParseConstraintDefinition(`FOREIGN KEY ("Odd""Col", plain) REFERENCES "Other Schema"."T(1)"("Id", x)`, true)
	require.NoError(t, err)
	assert.Equal(t, []string{`Odd"Col`, "plain"}, columns)
	assert.Equal(t, []string{"Id", "x"}, refs)
	columns, refs, err = tableParseConstraintDefinition("PRIMARY KEY (id, label)", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "label"}, columns)
	assert.Nil(t, refs)
	for _, definition := range []string{"CHECK", "PRIMARY KEY (id", "UNIQUE ()", "FOREIGN KEY (a)", "FOREIGN KEY (a) REFERENCES t"} {
		_, _, err := tableParseConstraintDefinition(definition, strings.HasPrefix(definition, "FOREIGN"))
		require.Error(t, err, definition)
	}

	assert.Equal(t, &tableIdentitySpec{seed: 1, step: 1}, tableParseIdentity(`"identity"(108123, 0, '1,1'::text)`))
	assert.Equal(t, &tableIdentitySpec{seed: -5, step: 10, generatedByDefault: true}, tableParseIdentity(`default_identity(108123, 2, '-5,10'::text)`))
	assert.Nil(t, tableParseIdentity("'identity'::character varying"))
	assert.Nil(t, tableParseIdentity("getdate()"))

	assert.Equal(t, "'it''s'", tableDefaultCanonical("('it''s'::character varying)"))
	assert.Equal(t, "'A B'", tableDefaultCanonical(" 'A B' :: text "))
	assert.Equal(t, "('a')||('b')", tableDefaultCanonical("('a') || ('b')"))
	assert.Equal(t, "0", tableDefaultCanonical("((0)::numeric(12,2))"))

	table := dataapi.Row{"owner": "admin", "diststyle": "9", "effective_diststyle": "12"}
	attributes := []dataapi.Row{{"column_name": "a", "data_type": "integer", "not_null": "t"}}
	details := []dataapi.Row{{"column_name": "a", "column_default": "", "encoding": "none", "distkey": "false", "sortkey": "0"}}
	catalog, err := tableCatalogFrom(table, attributes, details, nil, []dataapi.Row{{"sortkey1": " AUTO(SORTKEY) "}})
	require.NoError(t, err)
	assert.Equal(t, tableCatalog{
		owner: "admin", distStyle: "AUTO", effectiveDistStyle: "KEY", sortKeyListed: true, sortKey1: "AUTO(SORTKEY)",
		columns: []tableCatalogColumn{{name: "a", dataType: "integer", notNull: true, encoding: "RAW"}},
	}, catalog)
	hidden, err := tableCatalogFrom(dataapi.Row{"owner": "admin", "diststyle": "9", "effective_diststyle": ""}, attributes, details, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, hidden.effectiveDistStyle, "a PG_CLASS_INFO row the identity may not see leaves the effective style unknown")
	assert.False(t, hidden.sortKeyListed, "SVV_TABLE_INFO lists only tables with rows")
	for name, test := range map[string]struct {
		table               dataapi.Row
		attributes, details []dataapi.Row
		constraints         []dataapi.Row
		message             string
	}{
		"no owner":             {dataapi.Row{"diststyle": "9"}, attributes, details, nil, "no owner"},
		"unknown diststyle":    {dataapi.Row{"owner": "admin", "diststyle": "12"}, attributes, details, nil, "distribution style"},
		"no columns":           {table, nil, nil, nil, "different columns"},
		"missing details":      {table, attributes, []dataapi.Row{{"column_name": "b", "sortkey": "0"}}, nil, "does not report"},
		"bad sort position":    {table, attributes, []dataapi.Row{{"column_name": "a", "sortkey": "x"}}, nil, "sort key position"},
		"bad constraint":       {table, attributes, details, []dataapi.Row{{"constraint_name": "c", "constraint_type": "p", "definition": "PRIMARY KEY"}}, "constraint"},
		"dangling foreign key": {table, attributes, details, []dataapi.Row{{"constraint_name": "f", "constraint_type": "f", "definition": "FOREIGN KEY (a) REFERENCES t(b)"}}, "does not report"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tableCatalogFrom(test.table, test.attributes, test.details, test.constraints, nil)
			require.ErrorContains(t, err, test.message)
		})
	}
	_, _, err = tableReconcile(tableEventsModel(), tableCatalog{owner: "admin", distStyle: "KEY", columns: catalog.columns})
	require.ErrorContains(t, err, "without a distribution key")
}

// tableFakeCatalog reads a fake table into a catalog, as read does.
func tableFakeCatalog(t *testing.T, table tableFakeTable) tableCatalog {
	t.Helper()
	fake := &tableFake{tables: map[string]*tableFakeTable{"serving.events": &table}}
	read := func(sql string) []dataapi.Row {
		result, handled, err := fake.query(nil, dataapi.Connection{}, sql, map[string]string{"schema": "serving", "name": "events"})
		require.True(t, handled)
		require.NoError(t, err)
		return result
	}
	catalog, err := tableCatalogFrom(read("SELECT c.relname AS table_name")[0], read("SELECT a.attnum AS position"), read("SELECT column_name, column_default"),
		read("SELECT con.conname"), read("SELECT sortkey1 FROM svv_table_info"))
	require.NoError(t, err)
	return catalog
}

// TestTableReconcileKeepsConfiguredSpellings keeps configuration that the catalog spells differently, and
// reports catalog values where nothing was configured.
func TestTableReconcileKeepsConfiguredSpellings(t *testing.T) {
	events := tableFakeEvents()
	observed, names, err := tableReconcile(tableEventsModel(), tableFakeCatalog(t, events))
	require.NoError(t, err)
	assert.Equal(t, tableEventsModel(), observed, "a converged table reads back as configured")
	assert.Equal(t, map[string]string{tablePrimaryKeyKey: "events_pkey"}, names)

	imported, _, err := tableReconcile(tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events")), tableFakeCatalog(t, events))
	require.NoError(t, err)
	columns := tableTestColumnsOf(t, imported.Column)
	assert.Equal(t, "character varying(64)", columns[1].Type.ValueString())
	assert.Equal(t, "'none'::character varying", columns[1].Default.ValueString())
	assert.True(t, imported.Backup.IsNull(), "backup is not observable")
	assert.Equal(t, tableTestDistribution("KEY", "id"), imported.Distribution, "an explicit layout is reported after import")
	assert.Equal(t, tableTestSortKey("COMPOUND", "id"), imported.SortKey)

	empty := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Unique = tableTestUnique()
		m.ForeignKey = tableTestForeignKeys()
	})
	observed, _, err = tableReconcile(empty, tableFakeCatalog(t, events))
	require.NoError(t, err)
	assert.True(t, observed.Unique.Equal(tableTestUnique()), "configured empty sets stay empty")
	assert.True(t, observed.ForeignKey.Equal(tableTestForeignKeys()))

	interleaved := events
	interleaved.columns = []tableFakeColumn{events.columns[0], events.columns[1]}
	interleaved.columns[0].sortKey, interleaved.columns[1].sortKey = -1, 2
	observed, _, err = tableReconcile(tableEventsModel(), tableFakeCatalog(t, interleaved))
	require.NoError(t, err)
	assert.Equal(t, tableTestSortKey("INTERLEAVED", "id", "label"), observed.SortKey)
	assert.Equal(t, tableEffectiveSortKeyValue("INTERLEAVED", []string{"id", "label"}, false), observed.EffectiveSortKey)
}

// TestTableReadBlockPopulation reports the distribution and sort_key blocks only when the prior state had them or
// the catalog declares something other than AUTO, so an omitted block stays omitted until the layout drifts.
func TestTableReadBlockPopulation(t *testing.T) {
	auto := tableFakeEvents()
	auto.distStyle, auto.autoDistStyle, auto.autoSortKey = "9", "12", true
	omitted := tableWith(tableEventsModel(), tableAuto("id", "id"))
	explicitAuto := tableWith(omitted, func(m *tableModel) {
		m.Distribution, m.SortKey = tableTestDistribution("AUTO", ""), tableTestSortKey("AUTO")
	})
	for name, test := range map[string]struct {
		prior        tableModel
		table        func(*tableFakeTable)
		distribution types.Object
		sortKey      types.Object
	}{
		"omitted blocks stay omitted under AUTO": {omitted, nil, types.ObjectNull(tableDistributionAttributeTypes), types.ObjectNull(tableSortKeyAttributeTypes)},
		"configured AUTO blocks stay":            {explicitAuto, nil, tableTestDistribution("AUTO", ""), tableTestSortKey("AUTO")},
		"drift fills omitted blocks": {omitted, func(table *tableFakeTable) {
			table.distStyle, table.autoSortKey = "0", false
			table.columns[0].distKey = false
		}, tableTestDistribution("EVEN", ""), tableTestSortKey("COMPOUND", "id")},
		"sort key removed outside Terraform": {omitted, func(table *tableFakeTable) {
			table.autoSortKey = false
			table.columns[0].sortKey = 0
		}, types.ObjectNull(tableDistributionAttributeTypes), tableTestSortKey("NONE")},
		"import reports what is not AUTO": {tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events")), func(table *tableFakeTable) {
			table.distStyle = "8"
			table.columns[0].distKey = false
		}, tableTestDistribution("ALL", ""), types.ObjectNull(tableSortKeyAttributeTypes)},
	} {
		t.Run(name, func(t *testing.T) {
			table := *auto.clone()
			if test.table != nil {
				test.table(&table)
			}
			observed, _, err := tableReconcile(test.prior, tableFakeCatalog(t, table))
			require.NoError(t, err)
			assert.Equal(t, test.distribution, observed.Distribution)
			assert.Equal(t, test.sortKey, observed.SortKey)
		})
	}
}

// TestTableEffectiveFromCatalog derives the effective layout from PG_CLASS_INFO, the key column flags, and
// SVV_TABLE_INFO, and falls back to the configured sort key style when the view does not list the table.
func TestTableEffectiveFromCatalog(t *testing.T) {
	auto := func(change func(*tableFakeTable)) tableFakeTable {
		table := *tableFakeEvents().clone()
		table.distStyle, table.autoDistStyle, table.autoSortKey = "9", "12", true
		if change != nil {
			change(&table)
		}
		return table
	}
	omitted := tableWith(tableEventsModel(), tableAuto("id", "id"))
	none := tableWith(omitted, func(m *tableModel) { m.SortKey = tableTestSortKey("NONE") })
	unsorted := func(table *tableFakeTable) { table.columns[0].sortKey = 0 }
	for name, test := range map[string]struct {
		prior        tableModel
		table        tableFakeTable
		distribution types.Object
		sortKey      types.Object
	}{
		"explicit":           {tableEventsModel(), *tableFakeEvents().clone(), tableEffectiveDistributionValue("KEY", "id", false), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, false)},
		"chosen by Redshift": {omitted, auto(nil), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true)},
		"auto even": {omitted, auto(func(table *tableFakeTable) {
			table.autoDistStyle = "11"
			table.columns[0].distKey = false
		}), tableEffectiveDistributionValue("EVEN", "", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true)},
		"pg_class_info hidden with key": {omitted, auto(func(table *tableFakeTable) { table.autoDistStyle = "" }), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true)},
		"pg_class_info hidden without key": {omitted, auto(func(table *tableFakeTable) {
			table.autoDistStyle = ""
			table.columns[0].distKey = false
		}), tableEffectiveDistributionValue("", "", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true)},
		"no key chosen yet":         {omitted, auto(unsorted), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("NONE", nil, true)},
		"unlisted keeps auto":       {omitted, auto(func(table *tableFakeTable) { table.unlisted = true }), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true)},
		"unlisted keeps none":       {none, auto(func(table *tableFakeTable) { unsorted(table); table.unlisted = true }), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("NONE", nil, false)},
		"unlisted without key auto": {omitted, auto(func(table *tableFakeTable) { unsorted(table); table.unlisted = true }), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("NONE", nil, true)},
		"listed none": {omitted, auto(func(table *tableFakeTable) {
			unsorted(table)
			table.autoSortKey = false
		}), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("NONE", nil, false)},
		"unlisted explicit after import": {tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events")), auto(func(table *tableFakeTable) {
			table.unlisted = true
		}), tableEffectiveDistributionValue("KEY", "id", true), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, false)},
	} {
		t.Run(name, func(t *testing.T) {
			observed, _, err := tableReconcile(test.prior, tableFakeCatalog(t, test.table))
			require.NoError(t, err)
			assert.Equal(t, test.distribution, observed.EffectiveDistribution)
			assert.Equal(t, test.sortKey, observed.EffectiveSortKey)
		})
	}
}

// TestTableConverged reports every difference that verification must catch, matching columns by name.
func TestTableConverged(t *testing.T) {
	base := tableSpecMust(t, tableEventsModel())
	require.NoError(t, tableConverged(base, base))
	unknownEncoding := base
	unknownEncoding.columns = append([]tableColumnSpec{}, base.columns...)
	unknownEncoding.columns[0].encoding = ""
	require.NoError(t, tableConverged(unknownEncoding, base), "encodings left to Redshift are not compared")
	reordered := base
	reordered.columns = []tableColumnSpec{base.columns[1], base.columns[0]}
	require.NoError(t, tableConverged(base, reordered), "columns match by name")
	for name, change := range map[string]func(*tableSpec){
		"column count": func(s *tableSpec) { s.columns = s.columns[:1] },
		"column name":  func(s *tableSpec) { s.columns[1].name = "other" },
		"column type":  func(s *tableSpec) { s.columns[1].dataType = "integer" },
		"encoding":     func(s *tableSpec) { s.columns[1].encoding = "ZSTD" },
		"nullability": func(s *tableSpec) {
			nullable := false
			s.columns[1].nullable = &nullable
		},
		"default":     func(s *tableSpec) { s.columns[1].defaultSQL = "" },
		"identity":    func(s *tableSpec) { s.columns[0].identity = nil },
		"primary key": func(s *tableSpec) { s.primaryKey = nil },
		"unique":      func(s *tableSpec) { s.unique = [][]string{{"label"}} },
		"foreign key": func(s *tableSpec) {
			s.foreignKeys = []tableForeignKeySpec{{columns: []string{"id"}, refSchema: "s", refTable: "t", refColumns: []string{"id"}}}
		},
		"distribution": func(s *tableSpec) { s.distStyle, s.distKey = "EVEN", "" },
		"sort key":     func(s *tableSpec) { s.sortStyle, s.sortKey = "AUTO", nil },
		"owner":        func(s *tableSpec) { s.owner = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			observed := base
			observed.columns = append([]tableColumnSpec{}, base.columns...)
			change(&observed)
			require.Error(t, tableConverged(base, observed))
		})
	}
}

// TestTableReadErrors reports catalog failures and inconsistent rows instead of dropping the table from state.
func TestTableReadErrors(t *testing.T) {
	for name, rows := range map[string]func(sql string) ([]dataapi.Row, error){
		"ambiguous table": func(sql string) ([]dataapi.Row, error) {
			if strings.HasPrefix(sql, "SELECT c.relname") {
				return []dataapi.Row{{"owner": "a", "diststyle": "9"}, {"owner": "b", "diststyle": "9"}}, nil
			}
			return nil, nil
		},
		"columns disappear": func(sql string) ([]dataapi.Row, error) {
			if strings.HasPrefix(sql, "SELECT c.relname") {
				return []dataapi.Row{{"owner": "a", "diststyle": "9"}}, nil
			}
			return nil, nil
		},
		"sort key view denied": func(sql string) ([]dataapi.Row, error) {
			if strings.HasPrefix(sql, "SELECT sortkey1") {
				return nil, errors.New("permission denied for relation svv_table_info")
			}
			c := fullCatalog()
			return c.Query(context.Background(), dataapi.Connection{Database: "admin"}, sql, map[string]string{"schema": "serving", "name": "events"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := &tableResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				return rows(sql)
			}))}
			data := tableEventsModel()
			_, _, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}

// TestTableUpdateRefusesReplacementChanges fails an update whose current catalog needs a replacement, as when the
// plan was made without refreshing.
func TestTableUpdateRefusesReplacementChanges(t *testing.T) {
	c := fullCatalog()
	r := newTableResource()
	configureTestResource(t, r, c)
	planned := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Column = tableTestColumns(
			tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
			tableTestColumn("label", "varchar(32)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
		)
	})
	_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), "requires replacing the table")
	assert.Empty(t, c.writes)
}

// TestTableUpdateRefusesStaleDrops refuses to drop a column that was added after the plan, which the prior state
// does not know, and asks for a new plan instead.
func TestTableUpdateRefusesStaleDrops(t *testing.T) {
	c := fullCatalog()
	tableFakeNote(fakeState[*tableFake](c, "table").tables["serving.events"])
	r := newTableResource()
	configureTestResource(t, r, c)
	planned := tableWith(tableEventsModel(), func(m *tableModel) { m.Owner = types.StringValue("analyst") })
	_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), `column "note" was added to the table after the plan was made`)
	assert.Empty(t, c.writes)
}

// TestTableUpdateRefreshFailures stops an update whose re-read after the sort key change fails, before any
// encoding is changed from a definition that may be stale.
func TestTableUpdateRefreshFailures(t *testing.T) {
	note := tableTestColumn("note", "varchar(32)", tableEncoded("LZO"), tableNullable(true))
	prior := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Column = tableTestColumns(append(tableTestColumnsOf(t, m.Column), note)...)
	})
	planned := tableWith(prior, func(m *tableModel) { m.SortKey = tableTestSortKey("COMPOUND", "id", "note") })
	for name, test := range map[string]struct {
		after   func(c *catalog)
		failure error
		message string
	}{
		"table dropped": {after: func(c *catalog) { delete(fakeState[*tableFake](c, "table").tables, "serving.events") }, message: "disappeared"},
		"read fails":    {failure: errors.New("connection reset"), message: "connection reset"},
		"unsupported type": {after: func(c *catalog) {
			fakeState[*tableFake](c, "table").tables["serving.events"].columns[1].dataType = "money"
		}, message: "after the sort key change"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			tableFakeNote(fakeState[*tableFake](c, "table").tables["serving.events"])
			sorted := false
			r := &tableResource{testResourceClient(queryFunc(func(ctx context.Context, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if sorted && test.failure != nil {
					return nil, test.failure
				}
				rows, err := c.Query(ctx, connection, sql, parameters)
				if strings.Contains(sql, "ALTER COMPOUND SORTKEY") {
					sorted = true
					if test.after != nil {
						test.after(c)
					}
				}
				return rows, err
			}))}
			_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics[0].Detail(), test.message)
			assert.NotContains(t, c.writes, `ALTER TABLE "serving"."events" ALTER COLUMN "note" ENCODE LZO`)
		})
	}
}

// tableTestColumnsOf decodes a column list for tests that extend it.
func tableTestColumnsOf(t *testing.T, list types.List) []tableColumnModel {
	t.Helper()
	var columns []tableColumnModel
	require.False(t, list.ElementsAs(context.Background(), &columns, false).HasError())
	return columns
}

// TestTableCreateRejectsInvalidDefinitions fails before any SQL runs.
func TestTableCreateRejectsInvalidDefinitions(t *testing.T) {
	c := fullCatalog()
	r := newTableResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "create", nil, tableWith(tableEventsModel(), func(m *tableModel) { m.SortKey = tableTestSortKey("", "missing") }), nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestTableCreateDetectsDivergence fails when the created table does not match the plan, as when another
// definition already used the name.
func TestTableCreateDetectsDivergence(t *testing.T) {
	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")
	r := newTableResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "create", nil, tableWith(tableEventsModel(), func(m *tableModel) { m.Distribution = tableTestDistribution("EVEN", "") }), nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), "distribution")
}

// TestTableImport restores the identity fields and rejects incomplete identities.
func TestTableImport(t *testing.T) {
	r := newTableResource().(*tableResource)
	for id, valid := range map[string]bool{
		`{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"events"}`: true,
		`{"workgroup_name":"warehouse","database":"admin","name":"events"}`:                    false,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, !valid, resp.Diagnostics.HasError(), id)
	}
}

// tableProviderConfig is the provider block of the plan tests.
const tableProviderConfig = `
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
`

// Column blocks of the plan tests' serving.events table.
const (
	tableIDColumn = `
  column {
    name     = "id"
    type     = "bigint"
    encoding = "AZ64"
    identity {
      seed = 1
      step = 1
    }
  }`
	tableLabelColumn = `
  column {
    name     = "label"
    type     = "varchar(64)"
    encoding = "LZO"
    default  = "'none'"
  }`
)

// tableNoteColumn renders the note column block with a type.
func tableNoteColumn(dataType string) string {
	return fmt.Sprintf(`
  column {
    name = "note"
    type = %q
  }`, dataType)
}

// tableConfig renders the serving.events table with the given column blocks and extra attributes.
func tableConfig(columns, attributes string) string {
	return tableProviderConfig + `
resource "redshift_table" "events" {
  database = "admin"
  schema   = "serving"
  name     = "events"
` + columns + `
  primary_key {
    columns = ["id"]
  }
  distribution {
    key = "id"
  }
  sort_key {
    columns = ["id"]
  }` + attributes + `
}
`
}

// tablePlanProviders serves the provider against the fake catalog.
func tablePlanProviders(c *catalog) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})}
}

// tableExpectUpdate plans an in-place update.
func tableExpectUpdate() testresource.ConfigPlanChecks {
	return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate)}}
}

// TestTablePlans runs real plans against the fake: derived defaults plan no drift, a column inserted between
// others and reordered columns update in place without inconsistent results, widened columns update in place, and
// a narrowed column replaces the table.
func TestTablePlans(t *testing.T) {
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
			{Config: tableConfig(tableIDColumn+tableLabelColumn, ""), PlanOnly: true},
			{Config: tableConfig(tableIDColumn+tableNoteColumn("varchar(32)")+tableLabelColumn, ""), ConfigPlanChecks: tableExpectUpdate(),
				Check: testresource.ComposeTestCheckFunc(
					testresource.TestCheckResourceAttr("redshift_table.events", "column.1.name", "note"),
					testresource.TestCheckResourceAttr("redshift_table.events", "column.1.encoding", "LZO"),
					testresource.TestCheckResourceAttr("redshift_table.events", "column.2.name", "label"),
				)},
			{Config: tableConfig(tableIDColumn+tableNoteColumn("varchar(32)")+tableLabelColumn, ""), PlanOnly: true},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(32)"), ""), ConfigPlanChecks: tableExpectUpdate(),
				PreConfig: func() { reordered = len(c.writes) },
				Check: testresource.ComposeTestCheckFunc(
					testresource.TestCheckResourceAttr("redshift_table.events", "column.0.name", "label"),
					func(*terraform.State) error {
						if len(c.writes) != reordered {
							return fmt.Errorf("reordering columns wrote %v", c.writes[reordered:])
						}
						return nil
					},
				)},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(128)"), `
  owner = "analyst"`), ConfigPlanChecks: tableExpectUpdate(), Check: testresource.TestCheckResourceAttr("redshift_table.events", "owner", "analyst")},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(128)"), `
  owner = "analyst"`), PlanOnly: true},
			{Config: tableConfig(tableLabelColumn+tableIDColumn+tableNoteColumn("varchar(16)"), `
  owner = "analyst"`), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: testresource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionDestroyBeforeCreate),
			}}},
		},
	})
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ADD COLUMN "note" character varying(32)`)
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER COLUMN "note" TYPE character varying(128)`)
}

// tableAutoConfig renders serving.events without layout blocks, so Redshift manages distribution and sort key.
func tableAutoConfig(columns string) string {
	return tableProviderConfig + `
resource "redshift_table" "events" {
  database = "admin"
  schema   = "serving"
  name     = "events"
  column {
    name = "id"
    type = "bigint"
  }` + columns + `
}
`
}

// TestTableAutoLayoutPlans runs plans of a table that leaves its layout to Redshift: a distribution changed outside
// Terraform shows up and is set back to AUTO, and dropping a column Redshift chose as distribution and sort key
// detours through EVEN and no sort key before the drop.
func TestTableAutoLayoutPlans(t *testing.T) {
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
			{Config: tableAutoConfig(tableNoteColumn("varchar(32)")), PlanOnly: true},
			{Config: tableAutoConfig(tableNoteColumn("varchar(32)")), PreConfig: func() { table().distStyle = "0" }, ConfigPlanChecks: tableExpectUpdate(),
				Check: testresource.TestCheckNoResourceAttr("redshift_table.events", "distribution.style")},
			{Config: tableAutoConfig(""), ConfigPlanChecks: tableExpectUpdate(), PreConfig: func() {
				detour = len(c.writes)
				table().autoDistStyle = "12"
				table().columns[1].distKey, table().columns[1].sortKey = true, 1
			}},
			{Config: tableAutoConfig(""), PlanOnly: true},
		},
	})
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO`, "drift is planned back to AUTO")
	assert.Equal(t, []string{
		`ALTER TABLE "serving"."events" ALTER SORTKEY NONE`, `ALTER TABLE "serving"."events" ALTER DISTSTYLE EVEN`, `ALTER TABLE "serving"."events" DROP COLUMN "note"`,
		`ALTER TABLE "serving"."events" ALTER SORTKEY AUTO`, `ALTER TABLE "serving"."events" ALTER DISTSTYLE AUTO`,
	}, slices.DeleteFunc(slices.Clone(c.writes[detour:]), func(sql string) bool { return strings.HasPrefix(sql, "DROP TABLE") }))
}

// TestTableImportPlan imports the existing serving.events table: state then holds catalog spellings and no backup,
// so the first plan updates in place only to record the configured spellings, without any DDL, and the next plan
// is empty.
func TestTableImportPlan(t *testing.T) {
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
			{Config: config, PlanOnly: true},
		},
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Equal(t, []string{`DROP TABLE "serving"."events"`}, c.writes, "only the final destroy writes")
}
