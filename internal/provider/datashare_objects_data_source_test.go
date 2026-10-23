package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareObjectsDataSource, collection: true, filters: []string{"database", "datashare", "object_type"}})

// TestDatashareObjectsList filters members by datashare and object type, defaults to the provider database, and reports
// include_new only for schemas.
func TestDatashareObjectsList(t *testing.T) {
	for _, test := range []struct {
		name, share, kind string
		count             int
	}{
		{"all", "producer", "", 4},
		{"all_shares", "", "", 4},
		{"defaults", "", "", 4},
		{"functions", "producer", "function", 2},
		{"schemas", "producer", "schema", 1},
		{"missing", "missing", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			source := newDatashareObjectsDataSource()
			filters := map[string]string{"database": "admin", "datashare": test.share}
			if test.share == "" {
				delete(filters, "datashare")
			}
			if test.name == "defaults" {
				delete(filters, "database")
			}
			if test.kind != "" {
				filters["object_type"] = test.kind
			}
			state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), c)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Empty(t, c.writes)
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", filters)
			items := collectionResult(source, observed).Elements()
			require.Len(t, items, test.count)
			for _, item := range items {
				attributes := item.(types.Object).Attributes()
				assert.Equal(t, types.StringValue("producer"), attributes["datashare"])
				if attributes["object_type"] == types.StringValue("schema") {
					assert.Equal(t, types.BoolValue(true), attributes["include_new"])
				} else {
					assert.True(t, attributes["include_new"].IsNull())
				}
			}
		})
	}
}

// TestDatashareObjectsOpaqueNamesAndFailures returns member names verbatim without parsing them, and fails on an
// invalid include_new value or a catalog error.
func TestDatashareObjectsOpaqueNamesAndFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		row     sqlclient.Row
		err     error
		invalid bool
	}{
		{"opaque", sqlclient.Row{"object_name": `Odd.Schema.name(integer, character varying)`, "object_type": "function"}, nil, false},
		{"invalid_bool", sqlclient.Row{"object_name": "serving", "object_type": "schema", "include_new": "maybe"}, nil, true},
		{"failure", nil, errors.New("catalog unavailable"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := newDatashareObjectsDataSource()
			c := fullCatalog()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, map[string]string{"database": "admin", "datashare": "producer"}), queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
				if _, ok := params["share"]; !ok {
					return c.Query(ctx, connection, sql, params)
				}
				assert.Equal(t, "admin", connection.Database)
				return []sqlclient.Row{test.row}, test.err
			}))
			require.Equal(t, test.invalid, diagnostics.HasError(), "%v", diagnostics)
			if !test.invalid {
				var observed types.Object
				require.False(t, state.Get(context.Background(), &observed).HasError())
				items := collectionResult(source, observed).Elements()
				require.Len(t, items, 1)
				assert.Equal(t, types.StringValue(test.row["object_name"]), items[0].(types.Object).Attributes()["object_name"])
			}
		})
	}
}

// TestDatashareObjectsMissingDatabase returns an empty listing for a missing database without querying it for members.
func TestDatashareObjectsMissingDatabase(t *testing.T) {
	source := newDatashareObjectsDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, map[string]string{"database": "missing", "datashare": "producer"}), queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		assert.Contains(t, sql, "svv_redshift_databases", "an absent database must not be queried for members")
		return nil, nil
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Empty(t, collectionResult(source, observed).Elements())
	assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "missing", map[string]string{"database": "missing", "datashare": "producer"})
}
