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

// readTableLookup runs the table data source against the fake catalog.
func readTableLookup(t *testing.T, name string) (tableModel, diag.Diagnostics) {
	t.Helper()
	state, diagnostics := readSource(t, newTableDataSource(), tableNullModel(types.StringValue("admin"), types.StringValue("serving"), types.StringValue(name)), fullCatalog())
	var data tableModel
	if !diagnostics.HasError() {
		require.False(t, state.Get(context.Background(), &data).HasError())
	}
	return data, diagnostics
}

// TestTableLookup reports the catalog definition in catalog spellings with the resource's identity.
func TestTableLookup(t *testing.T) {
	data, diagnostics := readTableLookup(t, "events")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"schema": "serving", "name": "events"})
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, "KEY", data.DistStyle.ValueString())
	assert.Equal(t, "id", data.DistKey.ValueString())
	assert.True(t, data.PrimaryKey.Equal(tableTestList("id")))
	assert.True(t, data.Backup.IsNull(), "backup is not observable")
	var columns []tableColumnModel
	require.False(t, data.Columns.ElementsAs(context.Background(), &columns, false).HasError())
	require.Len(t, columns, 2)
	assert.Equal(t, "character varying(64)", columns[1].Type.ValueString())
	assert.Equal(t, "'none'::character varying", columns[1].Default.ValueString())
	assert.False(t, columns[0].Identity.IsNull())

	_, diagnostics = readTableLookup(t, "missing")
	assert.True(t, diagnostics.HasError(), "a missing table is an error")
}
