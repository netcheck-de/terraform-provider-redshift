package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalFunctionsDataSource, resource: newExternalFunctionResource, collection: true, filters: []string{"database", "schema", "name"}})

// TestExternalFunctionsListing lists only Lambda UDFs, with the values redshift_external_function reads, and honors
// the filters.
func TestExternalFunctionsListing(t *testing.T) {
	identity, items := routineListingRead(t, newExternalFunctionsDataSource(), nil, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin"}, identity)
	require.Len(t, items, 1, "the SQL UDF belongs to redshift_functions")
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "name": fakeExternalFunctionName, "arguments": []string{"CHARACTER VARYING"},
		"return_type": "CHARACTER VARYING", "volatility": "STABLE", "owner": "admin",
	}, items[0])

	identity, items = routineListingRead(t, newExternalFunctionsDataSource(), map[string]string{"schema": "public"}, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin", "schema": "public"}, identity)
	assert.Empty(t, items)

	_, items = routineListingRead(t, newExternalFunctionsDataSource(), map[string]string{"name": "other"}, fullCatalog())
	assert.Empty(t, items)
}
