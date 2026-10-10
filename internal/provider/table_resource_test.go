package provider

import (
	"context"
	"errors"
	"fmt"
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
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_table", map[string]replaceRule{
	"database":      replaceAlways,
	"schema":        replaceAlways,
	"name":          replaceAlways,
	"owner":         replaceNever,
	"columns":       replaceConditional("TestTableConditionalReplacement"),
	"primary_key":   replaceConditional("TestTableConditionalReplacement"),
	"unique":        replaceNever,
	"foreign_keys":  replaceNever,
	"diststyle":     replaceConditional("TestTableConditionalReplacement"),
	"distkey":       replaceConditional("TestTableConditionalReplacement"),
	"sortkey_style": replaceConditional("TestTableConditionalReplacement"),
	"sortkey":       replaceConditional("TestTableConditionalReplacement"),
	"backup":        replaceConditional("TestTableConditionalReplacement"),
})

var _ = registerLifecycleCase(lifecycleCase{
	name: "table", new: newTableResource, model: tableEventsModel(),
	absent: func(c *catalog) { delete(fakeState[*tableFake](c, "table").tables, "serving.events") },
})

var _ = registerValidateConfigCase("table", validateConfigCase{
	new:   newTableResource,
	valid: tableEventsModel(),
	invalid: tableWith(tableEventsModel(), func(m *tableModel) {
		m.DistKey = types.StringValue("missing")
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

// tableTestColumns builds the columns list.
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

// tableTestUnique builds the unique set.
func tableTestUnique(lists ...[]string) types.Set {
	elements := make([]attr.Value, len(lists))
	for i, names := range lists {
		elements[i] = tableTestList(names...)
	}
	return types.SetValueMust(types.ListType{ElemType: types.StringType}, elements)
}

// tableTestForeignKey builds one foreign key element.
func tableTestForeignKey(columns []string, refSchema, refTable string, refColumns ...string) attr.Value {
	return types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
		"columns": tableTestList(columns...), "references_schema": types.StringValue(refSchema),
		"references_table": types.StringValue(refTable), "references_columns": tableTestList(refColumns...),
	})
}

// tableTestForeignKeys builds the foreign key set.
func tableTestForeignKeys(keys ...attr.Value) types.Set {
	return types.SetValueMust(types.ObjectType{AttrTypes: tableForeignKeyAttributeTypes}, keys)
}

// tableWith returns a copy of model changed by change.
func tableWith(model tableModel, change func(*tableModel)) tableModel {
	change(&model)
	return model
}

// tableEventsModel is the configuration matching the fake's representative serving.events table.
func tableEventsModel() tableModel {
	model := tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events"))
	model.Owner = types.StringValue("admin")
	model.Columns = tableTestColumns(
		tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
		tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
	)
	model.PrimaryKey = tableTestList("id")
	model.DistStyle, model.DistKey = types.StringValue("KEY"), types.StringValue("id")
	model.SortKeyStyle, model.SortKey = types.StringValue("COMPOUND"), tableTestList("id")
	model.Backup = types.StringValue("YES")
	return model
}

// tableSpecMust validates a model for tests.
func tableSpecMust(t *testing.T, model tableModel) tableSpec {
	t.Helper()
	spec, err := tableSpecOf(model)
	require.NoError(t, err)
	return spec
}

// TestTableConditionalReplacement covers both branches of every conditionally replacing attribute: the changes
// ALTER TABLE makes in place and those it cannot make.
func TestTableConditionalReplacement(t *testing.T) {
	base := tableEventsModel()
	withColumns := func(columns ...tableColumnModel) tableModel {
		return tableWith(base, func(m *tableModel) { m.Columns = tableTestColumns(columns...) })
	}
	id := tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false))
	label := tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'"))
	note := func(options ...func(*tableColumnModel)) tableColumnModel {
		return tableTestColumn("note", "varchar(32)", append([]func(*tableColumnModel){tableEncoded("LZO"), tableNullable(true)}, options...)...)
	}
	withNote := withColumns(id, label, note())
	interleaved := func(m *tableModel) { m.SortKeyStyle = types.StringValue("INTERLEAVED") }
	for _, test := range []struct {
		name      string
		prev      tableModel
		plan      tableModel
		attribute string
		replace   bool
	}{
		{"append column", base, withNote, "columns", false},
		{"drop column", withNote, base, "columns", false},
		{"insert column before existing", base, withColumns(id, note(), label), "columns", true},
		{"reorder columns", withNote, withColumns(id, note(), label), "columns", true},
		{"append identity column", base, withColumns(id, label, tableTestColumn("seq", "integer", tableIdentity(1, 1, true))), "columns", true},
		{"append NOT NULL column without default", base, withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false))), "columns", true},
		{"append NOT NULL column with default", base, withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false), tableDefault("'xx'"))), "columns", false},
		{"new primary key column without default", base, tableWith(withColumns(id, label, tableTestColumn("code", "char(2)", tableNullable(false))), func(m *tableModel) {
			m.PrimaryKey = tableTestList("id", "code")
		}), "columns", true},
		{"widen varchar", withNote, withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), "columns", false},
		{"widen varchar alias", withNote, withColumns(id, label, tableTestColumn("note", "character varying(33)", tableEncoded("LZO"), tableNullable(true))), "columns", false},
		{"same type alias", withNote, withColumns(id, label, tableTestColumn("note", "nvarchar(32)", tableEncoded("LZO"), tableNullable(true))), "columns", false},
		{"widen varbyte", withColumns(id, label, tableTestColumn("blob", "varbyte(16)", tableEncoded("LZO"), tableNullable(true))),
			withColumns(id, label, tableTestColumn("blob", "varbinary(32)", tableEncoded("LZO"), tableNullable(true))), "columns", false},
		{"varbyte to varchar", withColumns(id, label, tableTestColumn("blob", "varbyte(16)", tableEncoded("LZO"), tableNullable(true))),
			withColumns(id, label, tableTestColumn("blob", "varchar(32)", tableEncoded("LZO"), tableNullable(true))), "columns", true},
		{"narrow varchar", withNote, withColumns(id, label, tableTestColumn("note", "varchar(16)", tableEncoded("LZO"), tableNullable(true))), "columns", true},
		{"change type", withNote, withColumns(id, label, tableTestColumn("note", "integer", tableEncoded("LZO"), tableNullable(true))), "columns", true},
		{"widen column with default", base, withColumns(id, tableTestColumn("label", "varchar(128)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'"))), "columns", true},
		{"widen constrained column", tableWith(withNote, func(m *tableModel) { m.Unique = tableTestUnique([]string{"note"}) }),
			tableWith(withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), func(m *tableModel) { m.Unique = tableTestUnique([]string{"note"}) }), "columns", true},
		{"widen foreign key column", tableWith(withNote, func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"note"}, "serving", "notes", "note"))
		}), tableWith(withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("LZO"), tableNullable(true))), func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"note"}, "serving", "notes", "note"))
		}), "columns", true},
		{"widen bytedict column", withColumns(id, label, note(tableEncoded("BYTEDICT"))), withColumns(id, label, tableTestColumn("note", "varchar(64)", tableEncoded("BYTEDICT"), tableNullable(true))), "columns", true},
		{"change encoding", withNote, withColumns(id, label, note(tableEncoded("ZSTD"))), "columns", false},
		{"change encoding with interleaved sort key", tableWith(withNote, interleaved), tableWith(withColumns(id, label, note(tableEncoded("ZSTD"))), interleaved), "columns", true},
		{"change nullability", withNote, withColumns(id, label, note(tableNullable(false))), "columns", true},
		{"add default", withNote, withColumns(id, label, note(tableDefault("'x'"))), "columns", true},
		{"default respelled", base, withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'::character varying"))), "columns", false},
		{"change default", base, withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'other'"))), "columns", true},
		{"change identity", base, withColumns(tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 2, false)), label), "columns", true},
		{"primary key on not null column", tableWith(withColumns(id, label, note(tableNullable(false))), func(m *tableModel) { m.PrimaryKey = types.ListNull(types.StringType) }),
			withColumns(id, label, note(tableNullable(false))), "primary_key", false},
		{"primary key on nullable column", base, tableWith(withColumns(id, tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(false), tableDefault("'none'"))),
			func(m *tableModel) { m.PrimaryKey = tableTestList("label") }), "primary_key", true},
		{"drop primary key", base, tableWith(base, func(m *tableModel) { m.PrimaryKey = types.ListNull(types.StringType) }), "primary_key", false},
		{"diststyle even", base, tableWith(base, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("EVEN"), types.StringNull() }), "diststyle", false},
		{"diststyle with interleaved sort key", tableWith(base, interleaved), tableWith(base, func(m *tableModel) {
			interleaved(m)
			m.DistStyle, m.DistKey = types.StringValue("ALL"), types.StringNull()
		}), "diststyle", true},
		{"distkey", withNote, tableWith(withNote, func(m *tableModel) { m.DistKey = types.StringValue("note") }), "distkey", false},
		{"distkey with interleaved sort key", tableWith(withNote, interleaved), tableWith(withNote, func(m *tableModel) {
			interleaved(m)
			m.DistKey = types.StringValue("note")
		}), "distkey", true},
		{"leave interleaved sort key", tableWith(base, interleaved), base, "sortkey_style", false},
		{"sort key auto", base, tableWith(base, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), "sortkey_style", false},
		{"interleaved sort key", base, tableWith(base, interleaved), "sortkey_style", true},
		{"interleaved sort key to auto", tableWith(base, interleaved), tableWith(base, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), "sortkey_style", false},
		{"auto sort key without the dropped key column", tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("note") }), tableWith(base, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), "sortkey_style", false},
		{"auto distribution without the dropped key column", tableWith(withNote, func(m *tableModel) { m.DistKey = types.StringValue("note") }), tableWith(base, func(m *tableModel) {
			m.DistStyle, m.DistKey = types.StringValue("AUTO"), types.StringNull()
		}), "diststyle", false},
		{"compound sort key columns", withNote, tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("id", "note") }), "sortkey", false},
		{"interleaved sort key columns", tableWith(withNote, interleaved), tableWith(withNote, func(m *tableModel) {
			interleaved(m)
			m.SortKey = tableTestList("id", "note")
		}), "sortkey", true},
		{"backup after import", tableWith(base, func(m *tableModel) { m.Backup = types.StringNull() }), tableWith(base, func(m *tableModel) { m.Backup = types.StringValue("NO") }), "backup", false},
		{"backup change", base, tableWith(base, func(m *tableModel) { m.Backup = types.StringValue("NO") }), "backup", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reasons := tableReplacements(tableSpecMust(t, test.prev), tableSpecMust(t, test.plan))
			assert.Equal(t, test.replace, reasons[test.attribute] != "", "%v", reasons)
			// The schema's plan modifier reaches the same decision from Terraform state and plan.
			r := newTableResource()
			state := testState(t, r, test.prev)
			assert.Equal(t, test.replace, tableRequiresReplace(context.Background(), state, tfsdk.Plan(testState(t, r, test.plan)), test.attribute))
		})
	}
	t.Run("undecodable state replaces", func(t *testing.T) {
		r := newTableResource()
		invalid := tableWith(base, func(m *tableModel) { m.DistKey = types.StringValue("missing") })
		assert.True(t, tableRequiresReplace(context.Background(), testState(t, r, invalid), tfsdk.Plan(testState(t, r, base)), "distkey"))
		assert.True(t, tableRequiresReplace(context.Background(), testState(t, r, base), tfsdk.Plan(testState(t, r, invalid)), "distkey"))
	})
}

// TestTableAlterCoverage keeps an update step for every in-place attribute.
func TestTableAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newTableResource(), tableAlterSteps(tablePhaseDropConstraints))
}

// TestTableSpecValidation rejects definitions Redshift would refuse before any SQL runs.
func TestTableSpecValidation(t *testing.T) {
	base := tableEventsModel()
	columns := func(columns ...tableColumnModel) func(*tableModel) {
		return func(m *tableModel) { m.Columns = tableTestColumns(columns...) }
	}
	for _, test := range []struct {
		name    string
		change  func(*tableModel)
		message string
	}{
		{"empty schema", func(m *tableModel) { m.Schema = types.StringValue("") }, "schema and name"},
		{"unknown name", func(m *tableModel) { m.Name = types.StringUnknown() }, "name must be known"},
		{"unknown columns", func(m *tableModel) {
			m.Columns = types.ListUnknown(types.ObjectType{AttrTypes: tableColumnAttributeTypes})
		}, "columns must be known"},
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
		{"unknown primary key column", func(m *tableModel) { m.PrimaryKey = tableTestList("missing") }, "does not declare"},
		{"repeated primary key column", func(m *tableModel) { m.PrimaryKey = tableTestList("id", "id") }, "distinct"},
		{"nullable primary key", func(m *tableModel) { m.PrimaryKey = tableTestList("label") }, "cannot be nullable"},
		{"unknown unique column", func(m *tableModel) { m.Unique = tableTestUnique([]string{"missing"}) }, "does not declare"},
		{"unknown foreign key column", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"missing"}, "serving", "accounts", "id"))
		}, "does not declare"},
		{"foreign key arity", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "accounts", "id", "other"))
		}, "as many references_columns"},
		{"foreign key without table", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "", "id"))
		}, "references_table"},
		{"key without distkey", func(m *tableModel) { m.DistKey = types.StringNull() }, "needs a distkey"},
		{"distkey without key", func(m *tableModel) { m.DistStyle = types.StringValue("EVEN") }, "requires diststyle KEY"},
		{"bad diststyle", func(m *tableModel) { m.DistStyle = types.StringValue("RANDOM") }, "diststyle"},
		{"auto with sort key", func(m *tableModel) { m.SortKeyStyle = types.StringValue("AUTO") }, "remove sortkey"},
		{"compound without columns", func(m *tableModel) { m.SortKey = types.ListNull(types.StringType) }, "needs sortkey columns"},
		{"interleaved limit", func(m *tableModel) {
			var many []tableColumnModel
			var names []string
			for i := range 9 {
				name := fmt.Sprintf("c%d", i)
				many, names = append(many, tableTestColumn(name, "int")), append(names, name)
			}
			m.Columns, m.PrimaryKey, m.DistStyle, m.DistKey = tableTestColumns(many...), types.ListNull(types.StringType), types.StringNull(), types.StringNull()
			m.SortKeyStyle, m.SortKey = types.StringValue("INTERLEAVED"), tableTestList(names...)
		}, "at most 8"},
		{"bad backup", func(m *tableModel) { m.Backup = types.StringValue("MAYBE") }, "backup"},
		{"uppercase schema", func(m *tableModel) { m.Schema = types.StringValue("Serving") }, "folds to lowercase"},
		{"uppercase name", func(m *tableModel) { m.Name = types.StringValue(`Odd"Events`) }, "folds to lowercase"},
		{"uppercase owner", func(m *tableModel) { m.Owner = types.StringValue("Admin") }, "folds to lowercase"},
		{"uppercase column", columns(tableTestColumn("Id", "int")), "folds to lowercase"},
		{"uppercase referenced table", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "Accounts", "id"))
		}, "folds to lowercase"},
		{"super distkey", columns(tableTestColumn("id", "super"), tableTestColumn("label", "varchar")), "does not allow in a key"},
		{"geometry sort key", func(m *tableModel) {
			m.Columns = tableTestColumns(tableTestColumn("id", "bigint"), tableTestColumn("shape", "geometry"))
			m.SortKey = tableTestList("id", "shape")
		}, "does not allow in a key"},
		{"unknown primary key", func(m *tableModel) { m.PrimaryKey = types.ListUnknown(types.StringType) }, "primary_key must be known"},
		{"unknown primary key column", func(m *tableModel) {
			m.PrimaryKey = types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})
		}, "known names"},
		{"empty unique", func(m *tableModel) { m.Unique = tableTestUnique([]string{}) }, "at least one column"},
		{"unknown unique", func(m *tableModel) { m.Unique = types.SetUnknown(types.ListType{ElemType: types.StringType}) }, "unique must be known"},
		{"unknown unique columns", func(m *tableModel) {
			m.Unique = types.SetValueMust(types.ListType{ElemType: types.StringType}, []attr.Value{types.ListUnknown(types.StringType)})
		}, "unique must be known"},
		{"unknown foreign keys", func(m *tableModel) {
			m.ForeignKeys = types.SetUnknown(types.ObjectType{AttrTypes: tableForeignKeyAttributeTypes})
		}, "foreign_keys must be known"},
		{"unknown foreign key columns", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
				"columns": types.ListUnknown(types.StringType), "references_schema": types.StringValue("s"), "references_table": types.StringValue("t"), "references_columns": tableTestList("id"),
			}))
		}, "foreign_keys.columns must be known"},
		{"unknown referenced table", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
				"columns": tableTestList("id"), "references_schema": types.StringValue("s"), "references_table": types.StringUnknown(), "references_columns": tableTestList("id"),
			}))
		}, "references_table must be known"},
		{"repeated referenced column", func(m *tableModel) {
			m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id", "label"}, "s", "t", "x", "x"))
		}, "distinct"},
		{"unknown distkey", func(m *tableModel) { m.DistKey = types.StringUnknown() }, "distkey must be known"},
		{"unknown sort key", func(m *tableModel) { m.SortKey = types.ListUnknown(types.StringType) }, "sortkey must be known"},
		{"bad sort key style", func(m *tableModel) { m.SortKeyStyle = types.StringValue("RANDOM") }, "sortkey_style"},
		{"unknown sort key column", func(m *tableModel) { m.SortKey = tableTestList("missing") }, "does not declare"},
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
		m.DistStyle, m.SortKeyStyle, m.Owner = types.StringNull(), types.StringNull(), types.StringUnknown()
	}))
	assert.Equal(t, "KEY", string(spec.distStyle), "a distkey implies KEY")
	assert.Equal(t, "COMPOUND", string(spec.sortStyle), "sort key columns imply COMPOUND")
	assert.Empty(t, spec.owner, "an unknown owner is left to the catalog")
	auto := tableSpecMust(t, tableWith(base, func(m *tableModel) {
		m.DistStyle, m.DistKey, m.SortKeyStyle, m.SortKey = types.StringNull(), types.StringNull(), types.StringNull(), types.ListNull(types.StringType)
	}))
	assert.Equal(t, "AUTO", string(auto.distStyle))
	assert.Equal(t, "AUTO", string(auto.sortStyle))
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

	table := dataapi.Row{"owner": "admin", "diststyle": "9"}
	attributes := []dataapi.Row{{"column_name": "a", "data_type": "integer", "not_null": "t"}}
	details := []dataapi.Row{{"column_name": "a", "column_default": "", "encoding": "none", "distkey": "false", "sortkey": "0"}}
	catalog, err := tableCatalogFrom(table, attributes, details, nil)
	require.NoError(t, err)
	assert.Equal(t, tableCatalog{owner: "admin", distStyle: "AUTO", columns: []tableCatalogColumn{{name: "a", dataType: "integer", notNull: true, encoding: "RAW"}}}, catalog)
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
			_, err := tableCatalogFrom(test.table, test.attributes, test.details, test.constraints)
			require.ErrorContains(t, err, test.message)
		})
	}
	_, _, err = tableReconcile(tableEventsModel(), tableCatalog{owner: "admin", distStyle: "KEY", columns: catalog.columns})
	require.ErrorContains(t, err, "without a distribution key")
}

// TestTableReconcileKeepsConfiguredSpellings keeps configuration that the catalog spells differently, and
// reports catalog values where nothing was configured.
func TestTableReconcileKeepsConfiguredSpellings(t *testing.T) {
	events := tableFakeEvents()
	rows := func(table tableFakeTable) tableCatalog {
		fake := &tableFake{tables: map[string]*tableFakeTable{"serving.events": &table}}
		read := func(sql string) []dataapi.Row {
			result, handled, err := fake.query(nil, dataapi.Connection{}, sql, map[string]string{"schema": "serving", "name": "events"})
			require.True(t, handled)
			require.NoError(t, err)
			return result
		}
		catalog, err := tableCatalogFrom(read("SELECT c.relname AS table_name")[0], read("SELECT a.attnum AS position"), read("SELECT column_name, column_default"), read("SELECT con.conname"))
		require.NoError(t, err)
		return catalog
	}
	observed, names, err := tableReconcile(tableEventsModel(), rows(events))
	require.NoError(t, err)
	assert.Equal(t, tableEventsModel(), observed, "a converged table reads back as configured")
	assert.Equal(t, map[string]string{tablePrimaryKeyKey: "events_pkey"}, names)

	imported, _, err := tableReconcile(tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue("events")), rows(events))
	require.NoError(t, err)
	var columns []tableColumnModel
	require.False(t, imported.Columns.ElementsAs(context.Background(), &columns, false).HasError())
	assert.Equal(t, "character varying(64)", columns[1].Type.ValueString())
	assert.Equal(t, "'none'::character varying", columns[1].Default.ValueString())
	assert.True(t, imported.Backup.IsNull(), "backup is not observable")

	empty := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Unique = tableTestUnique()
		m.ForeignKeys = tableTestForeignKeys()
	})
	observed, _, err = tableReconcile(empty, rows(events))
	require.NoError(t, err)
	assert.True(t, observed.Unique.Equal(tableTestUnique()), "configured empty lists stay empty")
	assert.True(t, observed.ForeignKeys.Equal(tableTestForeignKeys()))

	auto := events
	auto.distStyle, auto.columns = "9", []tableFakeColumn{events.columns[0], events.columns[1]}
	auto.columns[0].sortKey = 1
	catalog := rows(auto)
	catalog.autoSortKey = true
	observed, _, err = tableReconcile(tableEventsModel(), catalog)
	require.NoError(t, err)
	assert.Equal(t, "AUTO", observed.DistStyle.ValueString(), "AUTO(KEY(id)) reads as AUTO")
	assert.True(t, observed.DistKey.IsNull())
	assert.Equal(t, "AUTO", observed.SortKeyStyle.ValueString(), "AUTO(SORTKEY(id)) reads as AUTO")
	assert.True(t, observed.SortKey.IsNull())

	interleaved := events
	interleaved.columns = []tableFakeColumn{events.columns[0], events.columns[1]}
	interleaved.columns[0].sortKey, interleaved.columns[1].sortKey = -1, 2
	observed, _, err = tableReconcile(tableEventsModel(), rows(interleaved))
	require.NoError(t, err)
	assert.Equal(t, "INTERLEAVED", observed.SortKeyStyle.ValueString())
	assert.True(t, observed.SortKey.Equal(tableTestList("id", "label")))
}

// TestTableConverged reports every difference that verification must catch.
func TestTableConverged(t *testing.T) {
	base := tableSpecMust(t, tableEventsModel())
	require.NoError(t, tableConverged(base, base))
	unknownEncoding := base
	unknownEncoding.columns = append([]tableColumnSpec{}, base.columns...)
	unknownEncoding.columns[0].encoding = ""
	require.NoError(t, tableConverged(unknownEncoding, base), "encodings left to Redshift are not compared")
	for name, change := range map[string]func(*tableSpec){
		"column count": func(s *tableSpec) { s.columns = s.columns[:1] },
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
			data := tableWith(tableEventsModel(), func(m *tableModel) {
				m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
			})
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
		m.Columns = tableTestColumns(
			tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
			tableTestColumn("label", "varchar(32)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
		)
	})
	_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), "requires replacing the table")
	assert.Empty(t, c.writes)
}

// TestTableUpdateRefreshFailures stops an update whose re-read after the sort key change fails, before any
// encoding is changed from a definition that may be stale.
func TestTableUpdateRefreshFailures(t *testing.T) {
	note := tableTestColumn("note", "varchar(32)", tableEncoded("LZO"), tableNullable(true))
	prior := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Columns = tableTestColumns(append(tableTestColumnsOf(t, m.Columns), note)...)
	})
	planned := tableWith(prior, func(m *tableModel) { m.SortKey = tableTestList("id", "note") })
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

// tableTestColumnsOf decodes a columns list for tests that extend it.
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
	_, diagnostics := applyOperation(t, r, "create", nil, tableWith(tableEventsModel(), func(m *tableModel) { m.SortKey = tableTestList("missing") }), nil)
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
	_, diagnostics := applyOperation(t, r, "create", nil, tableWith(tableEventsModel(), func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("EVEN"), types.StringNull() }), nil)
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

// tableConfig renders the serving.events table with extra column definitions and attributes.
func tableConfig(columns, attributes string) string {
	return tableProviderConfig + `
resource "redshift_table" "events" {
  database = "admin"
  schema   = "serving"
  name     = "events"
  columns = [
    { name = "id", type = "bigint", encoding = "AZ64", identity = { seed = 1, step = 1 } },
    { name = "label", type = "varchar(64)", encoding = "LZO", default = "'none'" },` + columns + `
  ]
  primary_key = ["id"]
  distkey     = "id"
  sortkey     = ["id"]` + attributes + `
}
`
}

// TestTablePlans runs real plans against the fake: derived defaults plan no drift, appended and widened columns
// update in place, and a narrowed column replaces the table.
func TestTablePlans(t *testing.T) {
	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: tableConfig("", ""), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "diststyle", "KEY"),
				testresource.TestCheckResourceAttr("redshift_table.events", "sortkey_style", "COMPOUND"),
				testresource.TestCheckResourceAttr("redshift_table.events", "owner", "admin"),
				testresource.TestCheckResourceAttr("redshift_table.events", "columns.0.nullable", "false"),
				testresource.TestCheckResourceAttr("redshift_table.events", "columns.1.nullable", "true"),
				testresource.TestCheckResourceAttr("redshift_table.events", "columns.1.default", "'none'"),
			)},
			{Config: tableConfig("", ""), PlanOnly: true},
			{Config: tableConfig(`
    { name = "note", type = "varchar(32)" },`, ""), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}, Check: testresource.TestCheckResourceAttr("redshift_table.events", "columns.2.encoding", "LZO")},
			{Config: tableConfig(`
    { name = "note", type = "varchar(128)" },`, `
  owner = "analyst"`), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}, Check: testresource.TestCheckResourceAttr("redshift_table.events", "owner", "analyst")},
			{Config: tableConfig(`
    { name = "note", type = "varchar(128)" },`, `
  owner = "analyst"`), PlanOnly: true},
			{Config: tableConfig(`
    { name = "note", type = "varchar(16)" },`, `
  owner = "analyst"`), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: testresource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionDestroyBeforeCreate),
			}}},
		},
	})
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ADD COLUMN "note" character varying(32)`)
	assert.Contains(t, c.writes, `ALTER TABLE "serving"."events" ALTER COLUMN "note" TYPE character varying(128)`)
}

// TestTableImportPlan imports the existing serving.events table: state then holds catalog spellings and no backup,
// so the first plan updates in place only to record the configured spellings, without any DDL, and the next plan
// is empty.
func TestTableImportPlan(t *testing.T) {
	c := fullCatalog()
	config := tableConfig("", "") + `
import {
  to = redshift_table.events
  id = jsonencode({ workgroup_name = "warehouse", database = "admin", schema = "serving", name = "events" })
}
`
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: config, ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}},
			{Config: config, PlanOnly: true},
		},
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Equal(t, []string{`DROP TABLE "serving"."events"`}, c.writes, "only the final destroy writes")
}
