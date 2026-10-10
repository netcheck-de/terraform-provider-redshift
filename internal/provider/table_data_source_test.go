package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newTableDataSource, resource: newTableResource, selectors: []string{"database", "schema", "name"}})

// readTableLookup runs the table data source against the fake catalog after change adjusts serving.events.
func readTableLookup(t *testing.T, name string, change func(*tableFakeTable)) (tableModel, diag.Diagnostics) {
	t.Helper()
	c := fullCatalog()
	if change != nil {
		change(fakeState[*tableFake](c, "table").tables["serving.events"])
	}
	state, diagnostics := readSource(t, newTableDataSource(), tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue(name)), c)
	var data tableModel
	if !diagnostics.HasError() {
		require.False(t, state.Get(context.Background(), &data).HasError())
	}
	return data, diagnostics
}

// TestTableLookup reports the catalog definition in catalog spellings with the resource's identity, and always
// reports the declared layout, even when it is AUTO.
func TestTableLookup(t *testing.T) {
	data, diagnostics := readTableLookup(t, "events", nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"schema": "serving", "name": "events"})
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, tableTestDistribution("KEY", "id"), data.Distribution)
	assert.Equal(t, tableTestSortKey("COMPOUND", "id"), data.SortKey)
	assert.Equal(t, tableEffectiveDistributionValue("KEY", "id", false), data.EffectiveDistribution)
	assert.Equal(t, tableTestKey("id"), data.PrimaryKey)
	assert.True(t, data.Backup.IsNull(), "backup is not observable")
	columns := tableTestColumnsOf(t, data.Column)
	require.Len(t, columns, 2)
	assert.Equal(t, "CHARACTER VARYING(64)", columns[1].Type.ValueString())
	assert.Equal(t, "'none'::character varying", columns[1].Default.ValueString())
	assert.False(t, columns[0].Identity.IsNull())

	auto, diagnostics := readTableLookup(t, "events", func(table *tableFakeTable) {
		table.distStyle, table.autoDistStyle, table.autoSortKey = "9", "12", true
	})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, tableTestDistribution("AUTO", ""), auto.Distribution)
	assert.Equal(t, tableTestSortKey("AUTO"), auto.SortKey)
	assert.Equal(t, tableEffectiveDistributionValue("KEY", "id", true), auto.EffectiveDistribution)
	assert.Equal(t, tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, true), auto.EffectiveSortKey)

	_, diagnostics = readTableLookup(t, "missing", nil)
	assert.True(t, diagnostics.HasError(), "a missing table is an error")
}
