package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newFunctionsDataSource, resource: newFunctionResource, collection: true, filters: []string{"database", "schema", "name"}})

// TestFunctionsListing lists only SQL UDFs, with the values redshift_function reads, and honors the filters.
func TestFunctionsListing(t *testing.T) {
	identity, items := routineListingRead(t, newFunctionsDataSource(), nil, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin"}, identity)
	require.Len(t, items, 1, "the Lambda UDF belongs to redshift_external_functions")
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "public", "name": "f_example", "arguments": []string{"INTEGER"}, "signature": "INTEGER",
		"return_type": "INTEGER", "volatility": "IMMUTABLE", "language": "SQL", "owner": "admin",
	}, items[0])

	identity, items = routineListingRead(t, newFunctionsDataSource(), map[string]string{"database": "warehouse", "schema": "public", "name": "f_example"}, fullCatalog())
	assert.Equal(t, map[string]string{"database": "warehouse", "schema": "public", "name": "f_example"}, identity)
	require.Len(t, items, 1)
	assert.Equal(t, "warehouse", items[0]["database"])

	_, items = routineListingRead(t, newFunctionsDataSource(), map[string]string{"schema": "serving"}, fullCatalog())
	assert.Empty(t, items)
}
