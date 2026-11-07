package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newProceduresDataSource, resource: newProcedureResource, collection: true, filters: []string{"database", "schema", "name"}})

// TestProceduresListing lists every procedure overload with the values redshift_procedure reads and honors the
// filters.
func TestProceduresListing(t *testing.T) {
	_, items := routineListingRead(t, newProceduresDataSource(), nil, fullCatalog())
	require.Len(t, items, 2)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "public", "name": "sp_example", "signature": "INTEGER", "security": "INVOKER", "owner": "admin",
	}, items[0], "OUT arguments are not part of the signature")

	identity, items := routineListingRead(t, newProceduresDataSource(), map[string]string{"schema": "serving", "name": fakeProcedureName}, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin", "schema": "serving", "name": fakeProcedureName}, identity)
	require.Len(t, items, 1)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "name": fakeProcedureName, "signature": "INTEGER", "security": "DEFINER", "owner": "etl",
	}, items[0])
}
