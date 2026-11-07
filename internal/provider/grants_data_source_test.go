package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newGrantsDataSource, resource: newGrantResource, collection: true, filters: []string{"database_name", "schema_name", "role", "user"}})

// TestGrantsListing lists the scoped grants of a database or schema from the fake, in the connection
// redshift_grant reads them from, and reports missing databases and catalog failures as errors.
func TestGrantsListing(t *testing.T) {
	c := fullCatalog()

	var connections []string

	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		connections = append(connections, connection.Database)
		return c.Query(ctx, connection, sql, parameters)
	})

	items := listingItems(t, newGrantsDataSource(), map[string]string{"database_name": "analytics"}, client)
	assert.Equal(t, []string{"admin", "admin"}, connections, "a shared database is read from admin")
	require.Len(t, items, 2)
	assert.Equal(t, types.StringValue("example:readers"), items[0]["role"])
	assert.Equal(t, stringSet("SELECT"), items[0]["privileges"])
	assert.Equal(t, types.StringValue(grantFakeUser), items[1]["user"])
	assert.Equal(t, types.StringValue("TABLES"), items[1]["scope"])
	assert.Equal(t, stringSet("SELECT"), items[1]["grant_option_privileges"])

	items = listingItems(t, newGrantsDataSource(), map[string]string{"database_name": "analytics", "user": grantFakeUser}, client)
	require.Len(t, items, 1)
	assert.Equal(t, types.StringNull(), items[0]["role"])

	items = listingItems(t, newGrantsDataSource(), map[string]string{"database_name": "analytics", "schema_name": "serving", "role": "example:readers"}, client)
	require.Len(t, items, 2)
	assert.Equal(t, []types.String{types.StringValue("SCHEMA"), types.StringValue("TABLES")}, []types.String{items[0]["scope"].(types.String), items[1]["scope"].(types.String)})

	items = listingItems(t, newGrantsDataSource(), map[string]string{"database_name": "analytics", "schema_name": "serving"}, client)
	assert.Len(t, items, 3, "the datashare's schema USAGE is listed and the group's is not")

	local := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		connections = append(connections, connection.Database)

		if sql == `SELECT database_type FROM svv_redshift_databases WHERE database_name = :database` {
			return []sqlclient.Row{{"database_type": "local"}}, nil
		}

		assert.Equal(t, `SHOW GRANTS ON DATABASE "warehouse"`, sql)

		return nil, nil
	})
	connections = nil

	assert.Empty(t, listingItems(t, newGrantsDataSource(), map[string]string{"database_name": "warehouse"}, local))
	assert.Equal(t, []string{"admin", "warehouse"}, connections, "a local database is read in that database")

	assertListingFails(t, newGrantsDataSource(), map[string]string{"database_name": "missing"}, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, nil
	}))
	assertListingFails(t, newGrantsDataSource(), nil, nil)
}
