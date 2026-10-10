package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newConstraintsDataSource, collection: true, filters: []string{"database", "schema", "table", "constraint_type"}})

// TestConstraintsDataSource lists key constraints with ordered columns and foreign key targets.
func TestConstraintsDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/constraints", "database", newConstraintsDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, []string{"orders_pkey", "orders_label_key", "order_lines_order_fkey"}, discoveryNames(items, "name"))
	assert.Equal(t, []string{"PRIMARY KEY", "UNIQUE", "FOREIGN KEY"}, discoveryNames(items, "constraint_type"))
	assert.Equal(t, constraintsStrings([]string{"label", "Region"}), items[1]["columns"])
	assert.True(t, items[0]["referenced_table"].IsNull())
	assert.True(t, items[0]["referenced_columns"].IsNull())
	foreign := items[2]
	assert.Equal(t, types.StringValue("serving"), foreign["referenced_schema"])
	assert.Equal(t, types.StringValue("orders"), foreign["referenced_table"])
	assert.Equal(t, constraintsStrings([]string{"id"}), foreign["referenced_columns"])
	assert.Equal(t, types.StringValue("FOREIGN KEY (order_id) REFERENCES serving.orders(id)"), foreign["definition"])

	items = discoveryTranscript(t, "discovery/constraints", "primary_keys", newConstraintsDataSource, map[string]string{"database": "analytics", "schema": "serving", "table": "orders", "constraint_type": "PRIMARY KEY"})
	assert.Equal(t, []string{"orders_pkey"}, discoveryNames(items, "name"))

	items = discoveryTranscript(t, "discovery/constraints", "default_database", newConstraintsDataSource, map[string]string{"schema": `Odd"Schema's`})
	assert.Empty(t, items, "pg_constraint only covers the database the read connects to")

	var target sqlclient.Connection
	_, _, diagnostics := discoveryRead(t, newConstraintsDataSource, map[string]string{"database": "analytics"}, queryFunc(func(_ context.Context, connection sqlclient.Connection, _ string, _ map[string]string) ([]sqlclient.Row, error) {
		target = connection
		return nil, nil
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "analytics", target.Database, "constraints are read inside the listed database")
	_, _, diagnostics = discoveryRead(t, newConstraintsDataSource, map[string]string{"constraint_type": "CHECK"}, fullCatalog())
	assert.True(t, diagnostics.HasError(), "an unknown type must not widen the listing")

	discoveryFailures(t, newConstraintsDataSource, nil,
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "constraint_name": "orders_ck", "constraint_type": "", "definition": "CHECK (id > 0)"},
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "constraint_name": "orders_pkey", "constraint_type": "p", "definition": "PRIMARY KEY (id)"},
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "constraint_name": "", "constraint_type": "PRIMARY KEY", "definition": "PRIMARY KEY (id)"},
		sqlclient.Row{"schema_name": "serving", "table_name": "orders", "constraint_name": "orders_pkey", "constraint_type": "PRIMARY KEY", "definition": "PRIMARY KEY"},
		sqlclient.Row{"schema_name": "serving", "table_name": "lines", "constraint_name": "lines_fkey", "constraint_type": "FOREIGN KEY", "definition": "FOREIGN KEY (a) REFERENCES t"},
	)
}
