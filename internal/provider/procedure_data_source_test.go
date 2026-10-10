package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newProcedureDataSource, resource: newProcedureResource, selectors: []string{"database", "schema", "name", "arguments"}})

// TestProcedureLookup observes the populated procedure selected by its input types alone, leaving the settings the
// catalog does not report null.
func TestProcedureLookup(t *testing.T) {
	source := newProcedureDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "public", "name": "sp_example"})
	lookupValue(&data, "arguments", procedureArgumentList([]procedureArgumentModel{procedureTestArgument("", "", "int")}))
	c := fullCatalog()
	state, diagnostics := readSource(t, source, data, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	attributes := observed.Attributes()
	for name, expected := range map[string]string{"signature": "integer", "security": "INVOKER", "owner": "admin", "body": "BEGIN total := min_id * 2; END;"} {
		assert.Equal(t, types.StringValue(expected), attributes[name], name)
	}
	assert.True(t, attributes["nonatomic"].IsNull())
	assert.True(t, attributes["configuration"].IsNull())
	assert.Len(t, procedureArguments(attributes["arguments"].(types.List)), 1, "the configured selector is kept")
	assertLookupIdentity(t, attributes["id"].(types.String), "admin", map[string]string{"schema": "public", "name": "sp_example", "arguments": "integer"})
	assert.Empty(t, c.writes)

	lookupValue(&data, "name", types.StringValue("sp_missing"))
	_, diagnostics = readSource(t, source, data, fullCatalog())
	assert.True(t, diagnostics.HasError())
}
