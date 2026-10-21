package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageTableSQLUnknownDefinitions(t *testing.T) {
	m := tableMinimalModel()
	m.Schema = types.StringUnknown()
	_, err := tableSpecOf(m)
	require.ErrorContains(t, err, "schema")
	m = tableMinimalModel()
	m.Column = types.ListValueMust(types.ObjectType{AttrTypes: tableColumnAttributeTypes}, []attr.Value{types.ObjectUnknown(tableColumnAttributeTypes)})
	_, err = tableSpecOf(m)
	require.ErrorContains(t, err, "column must be known")
	c := tableTestColumn("id", "integer")
	c.Name = types.StringUnknown()
	_, err = tableColumnSpecOf(c)
	require.ErrorContains(t, err, "name")
	_, err = tableUniqueOf(types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{types.ObjectUnknown(tableKeyAttributeTypes)}))
	require.ErrorContains(t, err, "unique must be known")
	_, err = tableForeignKeysOf(tableTestForeignKeys(types.ObjectUnknown(tableForeignKeyAttributeTypes)))
	require.ErrorContains(t, err, "foreign_key must be known")
	for _, refs := range []types.Object{
		tableTestReferences(types.StringUnknown(), types.StringValue("t"), tableTestList("id")),
		tableTestReferences(types.StringValue("s"), types.StringValue("t"), types.ListUnknown(types.StringType)),
	} {
		_, err = tableForeignKeysOf(tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{"columns": tableTestList("payload"), "references": refs})))
		require.Error(t, err)
	}
	keys, err := tableForeignKeysOf(tableTestForeignKeys(tableTestForeignKey([]string{"a"}, "s", "t", "a"), tableTestForeignKey([]string{"b"}, "s", "t", "b")))
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	names := make([]string, tableMaxCompoundSortKeys+1)
	for i := range names {
		names[i] = fmt.Sprintf("c%d", i)
	}
	var spec tableSpec
	require.ErrorContains(t, spec.resolveSortKey(tableTestSortKey("COMPOUND", names...)), "at most")
	valid := tableMinimalModel()
	_, _, err = tableAlterStates(m, nil, valid)
	require.ErrorContains(t, err, "current table definition")
	_, _, err = tableAlterStates(valid, nil, m)
	require.Error(t, err)
}

func TestCoverageTableCatalogBoundaries(t *testing.T) {
	assert.Nil(t, tableParseIdentity("identity(123, 1, '99999999999999999999999999999,1'::text)"))
	_, _, err := tableIdentifierList("id", 0)
	require.ErrorContains(t, err, "expected a column list")
	_, _, err = tableIdentifierList("", 0)
	require.Error(t, err)
	m := tableMinimalModel()
	m.SortKey = types.ObjectUnknown(tableSortKeyAttributeTypes)
	assert.Empty(t, tablePriorSortStyle(m))
	_, err = tableSecurityDatashareStatement(tableSecurityModel{})
	require.ErrorContains(t, err, "nonempty schema")
}
