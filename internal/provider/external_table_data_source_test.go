package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalTableDataSource, resource: newExternalTableResource, selectors: []string{"database", "schema", "name"}})

// externalTableLookupState reads the table lookup against a fake catalog.
func externalTableLookupState(t *testing.T, c *catalog, name string) (externalTableModel, bool) {
	t.Helper()
	source := newExternalTableDataSource()
	config := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "example_external", "name": name})
	state, diagnostics := readSource(t, source, config, c)
	if diagnostics.HasError() {
		return externalTableModel{}, false
	}
	var data externalTableModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	return data, true
}

// TestExternalTableLookup reports the whole definition, including every table property, with the resource identity.
func TestExternalTableLookup(t *testing.T) {
	data, ok := externalTableLookupState(t, fullCatalog(), "Events")
	require.True(t, ok)
	assert.Equal(t, externalTableTestColumns("id", "INTEGER", "label", "VARCHAR(64)"), data.Columns)
	assert.Equal(t, externalTableTestColumns("event_date", "DATE"), data.PartitionKeys)
	assert.Equal(t, "TEXTFILE", data.StoredAs.ValueString())
	assert.Equal(t, ",", data.FieldDelimiter.ValueString())
	assert.Equal(t, externalTableTestMap("EXTERNAL", "TRUE", "skip.header.line.count", "1", "transient_lastDdlTime", "1700000000"), data.TableProperties)
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"schema": "example_external", "name": "Events"})
	_, ok = externalTableLookupState(t, fullCatalog(), "missing")
	assert.False(t, ok, "a missing table is an error")
	state, diagnostics := readSource(t, newExternalTableDataSource(), catalogLookupObject(t, newExternalTableDataSource(), map[string]string{"database": "admin", "schema": "example_external", "name": "events"}), &catalog{})
	assert.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull())
}
