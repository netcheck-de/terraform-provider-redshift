package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDefaultPrivilegesDataSource, resource: newDefaultPrivilegeResource, collection: true, filters: []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}})

// TestDefaultPrivilegesSQL pins the default privilege listing, with the implicit PUBLIC rule, with and without filters.
func TestDefaultPrivilegesSQL(t *testing.T) {
	render := func(owner, schema, grantee string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listDefaultPrivilegesQuery(owner, schema, grantee).Build()
			return sql, err
		}
	}
	checkSQL(t, "default_privileges", []sqlCase{{"unfiltered", render("", "", "")}, {"all_filters", render("loader", "serving", "readers")}})
}

// TestDefaultPrivilegesListing groups the catalog rows per rule in the selected database, maps catalog object types
// to the resource's, keeps the implicit PUBLIC EXECUTE rule as a database-wide FUNCTIONS element, and narrows the rules
// by object and grantee type.
func TestDefaultPrivilegesListing(t *testing.T) {
	var (
		database   string
		parameters map[string]string
	)

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, _ string, params map[string]string) ([]sqlclient.Row, error) {
		database, parameters = connection.Database, params

		return []sqlclient.Row{
			{"owner_name": "loader", "schema_name": "", "object_type": "FUNCTION", "grantee_name": "public", "grantee_type": "public", "privilege_type": "EXECUTE", "admin_option": "false"},
			{"owner_name": "loader", "schema_name": "", "object_type": "RELATION", "grantee_name": "readers", "grantee_type": "group", "privilege_type": "SELECT", "admin_option": "false"},
			{"owner_name": "loader", "schema_name": "serving", "object_type": "RELATION", "grantee_name": "analyst", "grantee_type": "user", "privilege_type": "INSERT", "admin_option": "f"},
			{"owner_name": "loader", "schema_name": "serving", "object_type": "RELATION", "grantee_name": "analyst", "grantee_type": "user", "privilege_type": "SELECT", "admin_option": "t"},
			{"owner_name": "loader", "schema_name": "serving", "object_type": "RELATION", "grantee_name": "analyst", "grantee_type": "user", "privilege_type": "RULE", "admin_option": "f"},
			{"owner_name": "loader", "schema_name": "serving", "object_type": "PROCEDURE", "grantee_name": "operators", "grantee_type": "role", "privilege_type": "EXECUTE", "admin_option": "f"},
		}, nil
	})

	items := listingItems(t, newDefaultPrivilegesDataSource(), map[string]string{"database_name": "warehouse"}, client)
	assert.Equal(t, "warehouse", database)
	assert.Empty(t, parameters)

	if assert.Len(t, items, 4) {
		assert.Equal(t, types.StringValue("FUNCTIONS"), items[0]["object_type"])
		assert.Equal(t, types.StringValue("PUBLIC"), items[0]["grantee_type"])
		assert.Equal(t, types.StringNull(), items[0]["schema_name"], "database-wide defaults have no schema")
		assert.Equal(t, types.StringValue("TABLES"), items[1]["object_type"])
		assert.Equal(t, types.StringValue("PROCEDURES"), items[2]["object_type"])
		assert.Equal(t, types.StringValue("serving"), items[3]["schema_name"])
		assert.Equal(t, types.StringValue("loader"), items[3]["owner"])
		assert.Equal(t, types.StringValue("warehouse"), items[3]["database_name"])
		assert.Equal(t, stringSet("INSERT", "RULE", "SELECT"), items[3]["privileges"])
		assert.Equal(t, stringSet("SELECT"), items[3]["grant_option_privileges"])
	}

	items = listingItems(t, newDefaultPrivilegesDataSource(), map[string]string{"object_type": "TABLES"}, client)
	assert.Equal(t, []string{"readers", "analyst"}, discoveryNames(items, "grantee"), "only the TABLES rules")

	items = listingItems(t, newDefaultPrivilegesDataSource(), map[string]string{"object_type": "TABLES", "grantee_type": "USER"}, client)
	assert.Equal(t, []string{"analyst"}, discoveryNames(items, "grantee"), "only the TABLES rules of users")

	items = listingItems(t, newDefaultPrivilegesDataSource(), map[string]string{"grantee_type": "ROLE"}, client)
	assert.Equal(t, []string{"operators"}, discoveryNames(items, "grantee"), "only the rules of roles")

	listingItems(t, newDefaultPrivilegesDataSource(), map[string]string{"owner": "loader", "schema_name": "serving", "grantee": "analyst"}, client)
	assert.Equal(t, "admin", database)
	assert.Equal(t, map[string]string{"owner": "loader", "schema": "serving", "grantee": "analyst"}, parameters)

	assertListingFails(t, newDefaultPrivilegesDataSource(), nil, nil)
}
