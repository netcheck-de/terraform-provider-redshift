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

var _ = registerParity(parityCase{source: newTablesDataSource, collection: true, filters: []string{"database", "schema", "table_type"}})

// TestTablesDataSource lists relations, tells materialized views from views, and filters by schema and type.
func TestTablesDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/tables", "database", newTablesDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, []string{"orders", "order_labels", "order_totals", "events"}, discoveryNames(items, "name"))
	assert.Equal(t, []string{"TABLE", "VIEW", "MATERIALIZED VIEW", "EXTERNAL TABLE"}, discoveryNames(items, "table_type"))
	assert.Equal(t, types.StringValue("Order facts."), items[0]["remarks"])
	assert.True(t, items[1]["remarks"].IsNull())
	assert.True(t, items[3]["owner"].IsNull(), "external tables have no owner")

	items = discoveryTranscript(t, "discovery/tables", "views", newTablesDataSource, map[string]string{"database": "analytics", "table_type": "VIEW"})
	assert.Equal(t, []string{"order_labels"}, discoveryNames(items, "name"), "materialized views are not plain views")

	items = discoveryTranscript(t, "discovery/tables", "materialized_views", newTablesDataSource, map[string]string{"database": "analytics", "schema": "serving", "table_type": "MATERIALIZED VIEW"})
	assert.Equal(t, []string{"order_totals"}, discoveryNames(items, "name"))
	assert.Equal(t, types.StringValue("etl"), items[0]["owner"])

	items = discoveryTranscript(t, "discovery/tables", "tables_only", newTablesDataSource, map[string]string{"database": "analytics", "table_type": "TABLE"})
	assert.Equal(t, []string{"orders"}, discoveryNames(items, "name"))

	items = discoveryTranscript(t, "discovery/tables", "quoted_schema", newTablesDataSource, map[string]string{"database": "analytics", "schema": `Odd"Schema's`})
	assert.Empty(t, items, "an empty listing skips the materialized view read")

	discoveryFailures(t, newTablesDataSource, nil, sqlclient.Row{"schema_name": "serving", "table_name": "", "table_type": "TABLE"})
}

// TestTablesMaterializedFailure reports a failed SVV_MV_INFO read instead of misclassifying views.
func TestTablesMaterializedFailure(t *testing.T) {
	_, _, diagnostics := discoveryRead(t, newTablesDataSource, nil, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_mv_info") {
			return nil, errors.New("STV state unavailable")
		}
		return []sqlclient.Row{{"schema_name": "serving", "table_name": "orders", "table_type": "VIEW"}}, nil
	}))
	require.True(t, diagnostics.HasError())
}

// TestTablesMaterializedHidden pins the documented limit for regular users: SVV_MV_INFO omits materialized views
// owned by others, so such a view stays VIEW and a MATERIALIZED VIEW filter drops it.
func TestTablesMaterializedHidden(t *testing.T) {
	catalog := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_mv_info") {
			return nil, nil
		}
		return []sqlclient.Row{{"schema_name": "serving", "table_name": "order_totals", "table_type": "VIEW", "owner": "etl"}}, nil
	})
	items, _, diagnostics := discoveryRead(t, newTablesDataSource, nil, catalog)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{"VIEW"}, discoveryNames(items, "table_type"))
	items, _, diagnostics = discoveryRead(t, newTablesDataSource, map[string]string{"table_type": "MATERIALIZED VIEW"}, catalog)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, items)
}
