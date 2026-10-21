package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableValidatePartiallyKnownNestedBlocks(t *testing.T) {
	unknownName := types.StringUnknown()
	partialNames := func(names ...types.String) types.List {
		elements := make([]attr.Value, len(names))
		for i := range names {
			elements[i] = names[i]
		}
		return types.ListValueMust(types.StringType, elements)
	}
	identity := func(seed, step types.Int64) types.Object {
		return types.ObjectValueMust(tableIdentityAttributeTypes, map[string]attr.Value{
			"seed": seed, "step": step, "generated_by_default": types.BoolUnknown(),
		})
	}
	withColumn := func(column tableColumnModel) tableModel {
		model := tableMinimalModel()
		model.Column = tableTestColumns(column)
		return model
	}
	withKey := func(columns types.List) tableModel {
		model := withColumn(tableTestColumn("id", "int", func(c *tableColumnModel) { c.Encoding = types.StringUnknown() }))
		model.PrimaryKey = types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": columns})
		return model
	}
	withForeign := func(columns types.List, schema, table types.String, referenced types.List) tableModel {
		model := withColumn(tableTestColumn("id", "int"))
		model.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{
			"columns": columns, "references": tableTestReferences(schema, table, referenced),
		}))
		return model
	}
	unknownColumn := types.ObjectUnknown(tableColumnAttributeTypes)
	listWithUnknown := func(model tableModel) tableModel {
		model.Column = types.ListValueMust(types.ObjectType{AttrTypes: tableColumnAttributeTypes}, append(model.Column.Elements(), unknownColumn))
		return model
	}
	tests := map[string]struct {
		model tableModel
		error string
	}{
		"known invalid type with unknown encoding": {
			model: withColumn(tableTestColumn("id", "serial", func(c *tableColumnModel) { c.Encoding = types.StringUnknown() })), error: "unsupported",
		},
		"known invalid type with unknown name": {
			model: withColumn(tableTestColumn("", "serial", func(c *tableColumnModel) { c.Name = unknownName })), error: "unsupported",
		},
		"known invalid column beside unknown element": {
			model: listWithUnknown(withColumn(tableTestColumn("id", "serial"))), error: "unsupported",
		},
		"duplicate known column names with unknown type": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.Column = tableTestColumns(tableTestColumn("id", "int"), tableTestColumn("id", "", func(c *tableColumnModel) { c.Type = types.StringUnknown() }))
			}), error: "declared twice",
		},
		"identity invalid known type with unknown step": {
			model: withColumn(tableTestColumn("id", "varchar(16)", func(c *tableColumnModel) { c.Identity = identity(types.Int64Value(1), types.Int64Unknown()) })), error: "INTEGER or BIGINT",
		},
		"identity zero step with unknown seed and type": {
			model: withColumn(tableTestColumn("id", "", func(c *tableColumnModel) {
				c.Type, c.Identity = types.StringUnknown(), identity(types.Int64Unknown(), types.Int64Value(0))
			})), error: "nonzero step",
		},
		"identity missing seed with unknown step": {
			model: withColumn(tableTestColumn("id", "int", func(c *tableColumnModel) { c.Identity = identity(types.Int64Null(), types.Int64Unknown()) })), error: "seed and a step",
		},
		"identity nullable with unknown seed": {
			model: withColumn(tableTestColumn("id", "int", tableNullable(true), func(c *tableColumnModel) { c.Identity = identity(types.Int64Unknown(), types.Int64Value(1)) })), error: "always NOT NULL",
		},
		"identity default with unknown seed": {
			model: withColumn(tableTestColumn("id", "int", tableDefault("1"), func(c *tableColumnModel) { c.Identity = identity(types.Int64Unknown(), types.Int64Value(1)) })), error: "also have a default",
		},
		"invalid default with unknown type": {
			model: withColumn(tableTestColumn("id", "", tableDefault("1; DROP TABLE x"), func(c *tableColumnModel) { c.Type = types.StringUnknown() })), error: "without ';'",
		},
		"primary duplicate known names among unknown elements": {
			model: withKey(partialNames(types.StringValue("id"), unknownName, types.StringValue("id"))), error: "distinct nonempty",
		},
		"primary missing column with unknown key element": {
			model: withKey(partialNames(unknownName, types.StringValue("missing"))), error: "does not declare",
		},
		"primary known nullable with unknown key element": {
			model: tableWith(withKey(partialNames(types.StringValue("id"), unknownName)), func(m *tableModel) {
				m.Column = tableTestColumns(tableTestColumn("id", "int", tableNullable(true)))
			}), error: "cannot be nullable",
		},
		"unique missing column with unrelated unknown type": {
			model: tableWith(withColumn(tableTestColumn("id", "", func(c *tableColumnModel) { c.Type = types.StringUnknown() })), func(m *tableModel) {
				m.Unique = tableTestUnique([]string{"missing"})
			}), error: "does not declare",
		},
		"unique duplicate known names with unknown column list": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.Column = types.ListUnknown(types.ObjectType{AttrTypes: tableColumnAttributeTypes})
				m.Unique = types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{
					types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": partialNames(types.StringValue("id"), unknownName, types.StringValue("id"))}),
				})
			}), error: "distinct nonempty",
		},
		"foreign mismatch with unknown list elements": {
			model: withForeign(partialNames(unknownName, types.StringValue("id")), types.StringUnknown(), types.StringUnknown(), partialNames(unknownName)), error: "as many references",
		},
		"foreign duplicate referenced known names": {
			model: withForeign(partialNames(unknownName, unknownName, unknownName), types.StringUnknown(), types.StringUnknown(), partialNames(types.StringValue("id"), unknownName, types.StringValue("id"))), error: "distinct nonempty",
		},
		"foreign missing schema independent of unknown table": {
			model: withForeign(partialNames(types.StringValue("id")), types.StringNull(), types.StringUnknown(), partialNames(unknownName)), error: "nonempty references schema",
		},
		"distribution incompatible style with unknown column type": {
			model: tableWith(withColumn(tableTestColumn("id", "", func(c *tableColumnModel) { c.Type = types.StringUnknown() })), func(m *tableModel) {
				m.Distribution = tableTestDistribution("EVEN", "id")
			}), error: "requires style KEY",
		},
		"distribution known unsupported type with unknown style": {
			model: tableWith(listWithUnknown(withColumn(tableTestColumn("payload", "super"))), func(m *tableModel) {
				m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "key": types.StringValue("payload")})
			}), error: "does not allow in a key",
		},
		"sort NONE with unknown name element": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringValue("NONE"), "columns": partialNames(unknownName)})
			}), error: "takes no columns",
		},
		"sort too many interleaved elements with unknown names": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringValue("INTERLEAVED"), "columns": partialNames(unknownName, unknownName, unknownName, unknownName, unknownName, unknownName, unknownName, unknownName, unknownName)})
			}), error: "at most 8",
		},
		"sort unsupported known type beside unknown column": {
			model: tableWith(listWithUnknown(withColumn(tableTestColumn("payload", "super"))), func(m *tableModel) { m.SortKey = tableTestSortKey("COMPOUND", "payload") }), error: "does not allow in a key",
		},
		"rename duplicate known sources with unknown names": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.Column = tableTestColumns(tableTestColumn("a", "int", tableRenamedFrom("old")), tableTestColumn("", "int", tableRenamedFrom("old"), func(c *tableColumnModel) { c.Name = unknownName }))
			}), error: "used more than once",
		},
		"valid dependent unknown identity type and options": {
			model: withColumn(tableTestColumn("id", "", func(c *tableColumnModel) {
				c.Type, c.Identity, c.Default, c.Nullable = types.StringUnknown(), identity(types.Int64Unknown(), types.Int64Unknown()), types.StringUnknown(), types.BoolUnknown()
			})),
		},
		"valid unknown name could satisfy missing reference": {
			model: listWithUnknown(withKey(partialNames(types.StringValue("missing")))),
		},
		"valid unknown name could avoid duplicate": {
			model: tableWith(withColumn(tableTestColumn("id", "int")), func(m *tableModel) {
				m.Column = tableTestColumns(tableTestColumn("id", "int"), tableTestColumn("", "int", func(c *tableColumnModel) { c.Name = unknownName }))
			}),
		},
		"valid unknown primary nullability": {
			model: tableWith(withKey(partialNames(types.StringValue("id"))), func(m *tableModel) {
				m.Column = tableTestColumns(tableTestColumn("id", "int", func(c *tableColumnModel) { c.Nullable = types.BoolUnknown() }))
			}),
		},
		"valid foreign unknown referenced list cardinality": {
			model: withForeign(partialNames(types.StringValue("id")), types.StringValue("public"), types.StringUnknown(), types.ListUnknown(types.StringType)),
		},
		"valid unknown distribution key": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringValue("KEY"), "key": types.StringUnknown()})
			}),
		},
		"valid unknown sort style with omitted columns": {
			model: tableWith(tableMinimalModel(), func(m *tableModel) {
				m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "columns": types.ListNull(types.StringType)})
			}),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := newTableResource().(*tableResource)
			config := tfsdk.Config(testState(t, r, test.model))
			require.False(t, config.Raw.IsFullyKnown(), "this must exercise partial validation")
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: config}, &response)
			if test.error == "" {
				assert.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			} else {
				require.True(t, response.Diagnostics.HasError())
				assert.Contains(t, response.Diagnostics[0].Detail(), test.error)
			}
		})
	}
}
