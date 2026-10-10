package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newSchemasDataSource, collection: true, filters: []string{"database", "schema_type"}})

// TestSchemasDataSource lists schemas of the provider database and of a selected database, by type.
func TestSchemasDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/schemas", "default_database", newSchemasDataSource, nil)
	assert.Empty(t, items, "the provider database has no schemas in the fake")

	items = discoveryTranscript(t, "discovery/schemas", "database", newSchemasDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, []string{"public", "serving", "spectrum"}, discoveryNames(items, "name"))
	assert.Equal(t, types.StringValue("glue_events"), items[2]["source_database"])
	assert.True(t, items[1]["source_database"].IsNull())
	assert.Equal(t, types.StringValue("analytics"), items[0]["database"])

	items = discoveryTranscript(t, "discovery/schemas", "external", newSchemasDataSource, map[string]string{"database": "analytics", "schema_type": "external"})
	assert.Equal(t, []string{"spectrum"}, discoveryNames(items, "name"))

	items = discoveryTranscript(t, "discovery/schemas", "quoted_database", newSchemasDataSource, map[string]string{"database": `Odd"Database's`})
	assert.Empty(t, items)

	items = discoveryTranscript(t, "discovery/schemas", "shared", newSchemasDataSource, map[string]string{"database": "consumer_db", "schema_type": "shared"})
	require.Len(t, items, 1)
	assert.True(t, items[0]["owner"].IsNull(), "a shared schema's producer owner is not resolved")

	var target sqlclient.Connection
	_, identity, diagnostics := discoveryRead(t, newSchemasDataSource, map[string]string{"database": "analytics"}, queryFunc(func(_ context.Context, connection sqlclient.Connection, _ string, _ map[string]string) ([]sqlclient.Row, error) {
		target = connection
		return nil, nil
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "admin", target.Database, "SVV_ALL_SCHEMAS is read from the administration database")
	assert.Equal(t, map[string]string{"workgroup_name": "warehouse", "database": "analytics"}, identity)

	discoveryFailures(t, newSchemasDataSource, nil, sqlclient.Row{"schema_name": ""})
}
