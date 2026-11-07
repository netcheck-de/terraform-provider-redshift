package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// TestGrantsSQL pins the SHOW GRANTS forms of the scoped grant listing.
func TestGrantsSQL(t *testing.T) {
	checkSQL(t, "grants", []sqlCase{
		{"on_database", func() string { return listGrantsStatement("warehouse", "") }},
		{"on_schema", func() string { return listGrantsStatement("warehouse", "serving") }},
		{"quoted_identifiers", func() string { return listGrantsStatement(`Odd"Database`, `Odd"Schema`) }},
	})
}

// TestGrantsItems keeps the rows each redshift_grant tuple reads, grouped per recipient and scope.
func TestGrantsItems(t *testing.T) {
	database := []sqlclient.Row{
		{"database_name": "warehouse", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "admin_option": "t", "privilege_scope": "TABLES"},
		{"database_name": "warehouse", "privilege_type": "INSERT", "identity_name": "analyst", "identity_type": "user", "admin_option": "f", "privilege_scope": "TABLES"},
		{"database_name": "warehouse", "privilege_type": "TEMP", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "DATABASE"},
		{"database_name": "warehouse", "privilege_type": "TEMP", "identity_name": "public", "identity_type": "public", "admin_option": "f", "privilege_scope": "DATABASE"},
		{"database_name": "warehouse", "privilege_type": "USAGE", "identity_name": "readers", "identity_type": "group", "admin_option": "f", "privilege_scope": "SCHEMAS"},
		{"database_name": "warehouse", "privilege_type": "SELECT", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "TABLE"},
		{"database_name": "warehouse", "schema_name": "serving", "privilege_type": "SELECT", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "TABLES"},
		{"database_name": "other", "privilege_type": "USAGE", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "SCHEMAS"},
	}

	items := grantsItems("warehouse", "", "", "", database)
	if assert.Len(t, items, 2, "group, PUBLIC, object-level, schema, and other database rows are skipped") {
		assert.Equal(t, types.StringValue("DATABASE"), items[0]["scope"])
		assert.Equal(t, types.StringValue("readers"), items[0]["role"])
		assert.Equal(t, types.StringNull(), items[0]["user"])
		assert.Equal(t, stringSet("TEMPORARY"), items[0]["privileges"])
		assert.Equal(t, types.StringValue("TABLES"), items[1]["scope"])
		assert.Equal(t, types.StringValue("analyst"), items[1]["user"])
		assert.Equal(t, types.StringNull(), items[1]["schema_name"])
		assert.Equal(t, stringSet("INSERT", "SELECT"), items[1]["privileges"])
		assert.Equal(t, stringSet("SELECT"), items[1]["grant_option_privileges"])
	}

	assert.Len(t, grantsItems("warehouse", "", "readers", "", database), 1, "the role filter keeps only the role's tuples")
	assert.Len(t, grantsItems("warehouse", "", "", "analyst", database), 1, "the user filter keeps only the user's tuples")
	assert.Empty(t, grantsItems("warehouse", "", "analyst", "", database), "a role filter does not match a user of the same name")

	schema := []sqlclient.Row{
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "serving", "object_type": "SCHEMA", "privilege_type": "USAGE", "identity_name": "ds:producer", "identity_type": "datashare", "admin_option": "f", "privilege_scope": "SCHEMA"},
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "serving", "object_type": "SCHEMA", "privilege_type": "EXECUTE", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "FUNCTIONS"},
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "serving", "object_type": "SCHEMA", "privilege_type": "EXECUTE", "identity_name": "ds:producer", "identity_type": "datashare", "admin_option": "f", "privilege_scope": "FUNCTIONS"},
		{"database_name": "warehouse", "schema_name": "other", "object_name": "other", "object_type": "SCHEMA", "privilege_type": "USAGE", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "SCHEMA"},
	}

	items = grantsItems("warehouse", "serving", "", "", schema)
	if assert.Len(t, items, 2, "datashares hold only SCHEMA and TABLES grants, and other schemas are skipped") {
		assert.Equal(t, types.StringValue("FUNCTIONS"), items[0]["scope"])
		assert.Equal(t, types.StringValue("readers"), items[0]["role"])
		assert.Equal(t, types.StringValue("serving"), items[0]["schema_name"])
		assert.Equal(t, types.StringValue("SCHEMA"), items[1]["scope"])
		assert.Equal(t, types.StringValue("producer"), items[1]["datashare"])
		assert.Equal(t, types.StringNull(), items[1]["role"])
		assert.Equal(t, stringSet(), items[1]["grant_option_privileges"])
	}
}
