package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareTablesDataSource, resource: newDatashareTableResource, collection: true, filters: []string{"database", "datashare", "schema"}})

// TestDatashareTablesList splits member names at the share's schemas, including one whose name contains a period,
// and filters by schema.
func TestDatashareTablesList(t *testing.T) {
	relation := func(schema, table string) map[string]attr.Value {
		return map[string]attr.Value{"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "schema": types.StringValue(schema), "table": types.StringValue(table)}
	}

	for _, test := range []struct {
		name     string
		filters  map[string]string
		expected []map[string]attr.Value
	}{
		{"all", nil, []map[string]attr.Value{relation("odd.schema", "events"), relation("serving", "table")}},
		{"schema", map[string]string{"datashare": "producer", "schema": "serving"}, []map[string]attr.Value{relation("serving", "table")}},
		{"dotted_schema", map[string]string{"schema": "odd.schema"}, []map[string]attr.Value{relation("odd.schema", "events")}},
		{"prefix_only", map[string]string{"schema": "odd"}, nil},
		{"missing_share", map[string]string{"datashare": "missing"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, items := readListing(t, newDatashareTablesDataSource(), test.filters, fullCatalog())
			assert.Equal(t, test.expected, items)
		})
	}
}

// TestDatashareTablesListUnsharedSchema splits a relation whose schema is not a share member at its first period.
func TestDatashareTablesListUnsharedSchema(t *testing.T) {
	_, items := readListing(t, newDatashareTablesDataSource(), nil, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"share_name": "producer", "object_type": "table", "object_name": "serving.events.v2"}}, nil
	}))
	require.Len(t, items, 1)
	assert.Equal(t, types.StringValue("serving"), items[0]["schema"])
	assert.Equal(t, types.StringValue("events.v2"), items[0]["table"])
}

// TestDatashareTablesListFailure reports a catalog error instead of an empty listing.
func TestDatashareTablesListFailure(t *testing.T) {
	source := newDatashareTablesDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), failingCatalog)
	assert.True(t, diagnostics.HasError())
}
