package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newTablesDataSource, resource: newTableResource, collection: true, filters: []string{"database", "schema"}})

// TestTablesListingTranscript lists the local tables of a database, optionally of one schema, and nothing for a database
// that is not local.
func TestTablesListingTranscript(t *testing.T) {
	items := discoveryTranscript(t, "discovery/tables", "database", newTablesDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, []string{"order_lines", "orders", "raw_orders"}, discoveryNames(items, "name"), "views are not tables")
	assert.Equal(t, []string{"serving", "serving", "staging"}, discoveryNames(items, "schema"))
	assert.Equal(t, types.StringValue("analytics"), items[0]["database"])
	assert.True(t, items[0]["owner"].IsNull(), "a missing owner is null")
	assert.Equal(t, types.StringValue("admin"), items[1]["owner"])

	items = discoveryTranscript(t, "discovery/tables", "schema", newTablesDataSource, map[string]string{"database": "analytics", "schema": "staging"})
	assert.Equal(t, []string{"raw_orders"}, discoveryNames(items, "name"))

	items = discoveryTranscript(t, "discovery/tables", "quoted_schema", newTablesDataSource, map[string]string{"database": "analytics", "schema": `Odd"Schema's`})
	assert.Empty(t, items)

	items = discoveryTranscript(t, "discovery/tables", "shared_database", newTablesDataSource, map[string]string{"database": "consumer_db"})
	assert.Empty(t, items, "a datashare consumer database has no local tables")

	discoveryFailures(t, newTablesDataSource, nil, sqlclient.Row{"schema_name": "serving", "table_name": ""})
}

// TestTablesListReportsDatabaseLookupFailure reports a failing database lookup instead of an empty listing.
func TestTablesListReportsDatabaseLookupFailure(t *testing.T) {
	_, _, diagnostics := discoveryRead(t, newTablesDataSource, map[string]string{"database": "other"}, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, string(tablesSource)) {
			return []sqlclient.Row{{"schema_name": "public", "table_name": "orders"}}, nil
		}

		return nil, errors.New("database metadata unavailable")
	}))
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "database metadata unavailable")
}
