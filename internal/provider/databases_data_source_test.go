package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatabasesDataSource, collection: true, filters: []string{"database_type", "name_like"}})

// TestDatabasesDataSource lists databases with and without filters and pins the bound values.
func TestDatabasesDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/databases", "all", newDatabasesDataSource, nil)
	assert.Equal(t, []string{"admin", "analytics", "consumer_db"}, discoveryNames(items, "name"))
	assert.Equal(t, types.StringValue("SERIALIZABLE"), items[1]["isolation_level"])
	assert.Equal(t, types.StringValue("admin"), items[1]["owner"])
	assert.True(t, items[2]["owner"].IsNull(), "a missing owner is null, not empty")
	assert.True(t, items[2]["isolation_level"].IsNull())
	assert.Equal(t, types.StringValue("SHARED"), items[2]["database_type"])

	items = discoveryTranscript(t, "discovery/databases", "filtered", newDatabasesDataSource, map[string]string{"database_type": "local", "name_like": "ana%"})
	assert.Equal(t, []string{"analytics"}, discoveryNames(items, "name"))

	items = discoveryTranscript(t, "discovery/databases", "quoted_pattern", newDatabasesDataSource, map[string]string{"name_like": `it's\_"%`})
	assert.Empty(t, items, "no match is an empty list")

	_, identity, diagnostics := discoveryRead(t, newDatabasesDataSource, map[string]string{"database_type": "shared"}, fullCatalog())
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, map[string]string{"workgroup_name": "warehouse", "database": "admin", "database_type": "shared"}, identity)

	discoveryFailures(t, newDatabasesDataSource, nil, sqlclient.Row{"database_name": "", "database_type": "local"})
}
