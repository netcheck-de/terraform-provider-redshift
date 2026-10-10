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

var _ = registerParity(parityCase{source: newColumnGrantsDataSource, collection: true, filters: []string{"database_name", "schema_name", "object_name", "grantee", "grantee_type"}})

// TestColumnGrantsListing lists the fake's column privileges, filtered and unfiltered, in the selected database.
func TestColumnGrantsListing(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*columnGrantFake](c, "column_grant")
	fake.set(`GROUP "readers"`, map[string][]string{"SELECT": {"id"}})
	var database string
	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		database = connection.Database
		return c.Query(ctx, connection, sql, parameters)
	})
	for _, test := range []struct {
		name     string
		filters  map[string]string
		database string
		items    [][]string
	}{
		{"unfiltered", nil, "admin", [][]string{{"id", "SELECT", "readers", "GROUP"}, {"id", "SELECT", "example:readers", "ROLE"}, {"label", "SELECT", "example:readers", "ROLE"}, {"label", "UPDATE", "example:readers", "ROLE"}}},
		{"group", map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "grantee_type": "GROUP"}, "warehouse", [][]string{{"id", "SELECT", "readers", "GROUP"}}},
		{"role", map[string]string{"grantee": "example:readers", "grantee_type": "ROLE"}, "admin", [][]string{{"id", "SELECT", "example:readers", "ROLE"}, {"label", "SELECT", "example:readers", "ROLE"}, {"label", "UPDATE", "example:readers", "ROLE"}}},
		{"empty", map[string]string{"object_name": "missing"}, "admin", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := newColumnGrantsDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.database, database)
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			items := observed.Attributes()[collectionItems].(types.List).Elements()
			require.Len(t, items, len(test.items))
			for index, expected := range test.items {
				item := items[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(test.database), item["database_name"])
				assert.Equal(t, types.StringValue("serving"), item["schema_name"])
				assert.Equal(t, types.StringValue("events"), item["object_name"])
				assert.Equal(t, []string{expected[0], expected[1], expected[2], expected[3]}, []string{
					item["column_name"].(types.String).ValueString(), item["privilege"].(types.String).ValueString(),
					item["grantee"].(types.String).ValueString(), item["grantee_type"].(types.String).ValueString(),
				})
			}
		})
	}
	source := newColumnGrantsDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError(), "catalog failures are errors")
}
