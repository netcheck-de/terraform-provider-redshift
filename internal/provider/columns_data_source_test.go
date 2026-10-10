package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newColumnsDataSource, collection: true, filters: []string{"database", "schema", "table"}})

// TestColumnsDataSource lists columns in position order with their types, nullability, and defaults.
func TestColumnsDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/columns", "table", newColumnsDataSource, map[string]string{"database": "analytics", "schema": "serving", "table": "orders"})
	assert.Equal(t, []string{"id", "label"}, discoveryNames(items, "name"))
	id, label := items[0], items[1]
	assert.Equal(t, types.Int64Value(1), id["ordinal_position"])
	assert.Equal(t, types.StringValue("integer"), id["data_type"])
	assert.Equal(t, types.Int64Value(32), id["numeric_precision"])
	assert.Equal(t, types.Int64Value(0), id["numeric_scale"])
	assert.True(t, id["character_maximum_length"].IsNull())
	assert.Equal(t, types.BoolValue(false), id["nullable"])
	assert.True(t, id["default"].IsNull())
	assert.Equal(t, types.StringValue("Order key."), id["remarks"])
	assert.Equal(t, types.Int64Value(64), label["character_maximum_length"])
	assert.Equal(t, types.BoolValue(true), label["nullable"])
	assert.Equal(t, types.StringValue("'none'::character varying"), label["default"])
	assert.Equal(t, types.StringValue("serving"), label["schema"])
	assert.Equal(t, types.StringValue("orders"), label["table"])

	items = discoveryTranscript(t, "discovery/columns", "database", newColumnsDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, []string{"id", "label", "payload"}, discoveryNames(items, "name"))
	assert.True(t, items[2]["nullable"].IsNull(), "missing nullability information stays unknown")

	items = discoveryTranscript(t, "discovery/columns", "quoted_table", newColumnsDataSource, map[string]string{"schema": `Odd"Schema`, "table": `it's\table`})
	assert.Empty(t, items)

	discoveryFailures(t, newColumnsDataSource, nil,
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "column_name": ""},
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "column_name": "id", "ordinal_position": "first"},
	)
}
