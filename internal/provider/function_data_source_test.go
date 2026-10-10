package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newFunctionDataSource, resource: newFunctionResource, selectors: []string{"database", "schema", "name", "arguments"}})

// TestFunctionLookup observes the populated overload by its input types, with the resource's identity.
func TestFunctionLookup(t *testing.T) {
	source := newFunctionDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "public", "name": "f_example"})
	lookupValue(&data, "arguments", types.ListValueMust(types.StringType, []attr.Value{types.StringValue("int4")}))
	c := fullCatalog()
	state, diagnostics := readSource(t, source, data, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	attributes := observed.Attributes()
	for name, expected := range map[string]string{"signature": "integer", "return_type": "integer", "volatility": "IMMUTABLE", "language": "sql", "body": "SELECT $1 + 1", "owner": "admin"} {
		assert.Equal(t, types.StringValue(expected), attributes[name], name)
	}
	assert.Equal(t, types.StringValue(definitionFingerprint("SELECT $1 + 1")), attributes["definition_fingerprint"])
	assertLookupIdentity(t, attributes["id"].(types.String), "admin", map[string]string{"schema": "public", "name": "f_example", "arguments": "integer"})
	assert.Empty(t, c.writes)

	// Another overload of the same name is not found, which is an error for an object lookup.
	lookupValue(&data, "arguments", types.ListNull(types.StringType))
	_, diagnostics = readSource(t, source, data, fullCatalog())
	assert.True(t, diagnostics.HasError())
}
