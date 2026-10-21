package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

func TestCoverageTableKnownValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*tableModel)
		want   string
	}{
		{"empty schema", func(m *tableModel) { m.Schema = types.StringValue("") }, "must be nonempty"},
		{"mixed schema", func(m *tableModel) { m.Schema = types.StringValue("Mixed") }, "lowercase"},
		{"backup", func(m *tableModel) { m.Backup = types.StringValue("INVALID") }, "backup"},
		{"missing columns", func(m *tableModel) { m.Column = types.ListNull(types.ObjectType{AttrTypes: tableColumnAttributeTypes}) }, "column block"},
		{"empty columns", func(m *tableModel) { m.Column = tableTestColumns() }, "between 1"},
		{"null column block", func(m *tableModel) {
			m.Column = types.ListValueMust(types.ObjectType{AttrTypes: tableColumnAttributeTypes}, []attr.Value{types.ObjectNull(tableColumnAttributeTypes)})
		}, "invalid column block"},
		{"null unique block", func(m *tableModel) {
			m.Unique = types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{types.ObjectNull(tableKeyAttributeTypes)})
		}, "invalid unique block"},
		{"null foreign key block", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(types.ObjectNull(tableForeignKeyAttributeTypes))
		}, "invalid foreign_key block"},
		{"rename collision", func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "int", func(c *tableColumnModel) { c.RenamedFrom = types.StringValue("id") }))
		}, "also a configured"},
		{"empty primary", func(m *tableModel) { m.PrimaryKey = tableTestKey() }, "at least one"},
		{"null key name", func(m *tableModel) {
			m.PrimaryKey = types.ObjectValueMust(tableKeyAttributeTypes, map[string]attr.Value{"columns": types.ListValueMust(types.StringType, []attr.Value{types.StringNull()})})
		}, "distinct nonempty"},
		{"mixed key", func(m *tableModel) { m.PrimaryKey = tableTestKey("Payload") }, "lowercase"},
		{"foreign local missing", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"absent"}, "s", "t", "id"))
		}, "does not declare"},
		{"foreign references missing", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{"columns": tableTestList("payload"), "references": types.ObjectNull(tableReferencesAttributeTypes)}))
		}, "references block"},
		{"foreign mixed schema", func(m *tableModel) {
			m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"payload"}, "Mixed", "t", "id"))
		}, "lowercase"},
		{"distribution invalid", func(m *tableModel) { m.Distribution = tableTestDistribution("INVALID", "") }, "distribution.style"},
		{"distribution key missing", func(m *tableModel) { m.Distribution = tableTestDistribution("KEY", "") }, "needs a key"},
		{"sort invalid", func(m *tableModel) { m.SortKey = tableTestSortKey("INVALID") }, "sort_key.style"},
		{"sort columns missing", func(m *tableModel) { m.SortKey = tableTestSortKey("COMPOUND") }, "needs sort_key.columns"},
		{"sort too wide", func(m *tableModel) {
			names := make([]string, tableMaxCompoundSortKeys+1)
			for i := range names {
				names[i] = fmt.Sprintf("c%d", i)
			}
			m.SortKey = tableTestSortKey("COMPOUND", names...)
		}, "at most"},
		{"column empty", func(m *tableModel) { m.Column = tableTestColumns(tableTestColumn("", "int")) }, "nonempty"},
		{"column mixed", func(m *tableModel) { m.Column = tableTestColumns(tableTestColumn("Mixed", "int")) }, "lowercase"},
		{"column encoding", func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "int", tableEncoded("INVALID")))
		}, "encoding"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := tableMinimalModel()
			test.change(&model)
			require.ErrorContains(t, tableValidateKnown(model), test.want)
		})
	}
	t.Run("unknown nested constraints defer", func(t *testing.T) {
		model := tableMinimalModel()
		model.Unique = types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{types.ObjectUnknown(tableKeyAttributeTypes)})
		model.ForeignKey = types.SetValueMust(types.ObjectType{AttrTypes: tableForeignKeyAttributeTypes}, []attr.Value{types.ObjectUnknown(tableForeignKeyAttributeTypes)})
		require.NoError(t, tableValidateKnown(model))
		model.ForeignKey = tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{"columns": tableTestList("payload"), "references": types.ObjectUnknown(tableReferencesAttributeTypes)}))
		require.NoError(t, tableValidateKnown(model))
	})
}
