package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// TestObjectGrantsSQL pins the SHOW GRANTS forms of the object grant listing and its rejected filter combinations.
func TestObjectGrantsSQL(t *testing.T) {
	render := func(filters objectGrantsFilters) func() (string, error) {
		return func() (string, error) {
			if filters.database == "" {
				filters.database = "warehouse"
			}

			return readObjectGrantsStatement(filters)
		}
	}
	checkSQL(t, "object_grants", []sqlCase{
		{"on_database", render(objectGrantsFilters{objectType: "DATABASE"})},
		{"on_schema", render(objectGrantsFilters{objectType: "SCHEMA", schema: "serving"})},
		{"on_table", render(objectGrantsFilters{objectType: "TABLE", schema: "serving", object: "events", grantee: "readers", granteeType: "GROUP"})},
		{"on_table_lower_case_kind", render(objectGrantsFilters{objectType: "table", schema: "serving", object: "events"})},
		{"on_template", render(objectGrantsFilters{objectType: "TEMPLATE", schema: "serving", object: "csv_load"})},
		{"for_user", render(objectGrantsFilters{grantee: "analyst", granteeType: "USER"})},
		{"for_role", render(objectGrantsFilters{grantee: "example:readers", granteeType: "ROLE", schema: "serving"})},
		{"quoted_identifiers", render(objectGrantsFilters{database: `Odd"Database`, objectType: "TABLE", schema: `Odd"Schema`, object: `Odd"Table`})},
		{"quoted_role", render(objectGrantsFilters{database: `Odd"Database`, grantee: `Odd"O'Reilly\Role`, granteeType: "ROLE"})},
		{"for_group_rejected", render(objectGrantsFilters{grantee: "readers", granteeType: "GROUP"})},
		{"nothing_selected", render(objectGrantsFilters{})},
		{"unsupported_object_type", render(objectGrantsFilters{objectType: "FUNCTION", schema: "serving", object: "f"})},
		{"database_with_schema", render(objectGrantsFilters{objectType: "DATABASE", schema: "serving"})},
		{"schema_without_schema_name", render(objectGrantsFilters{objectType: "SCHEMA"})},
		{"schema_with_object_name", render(objectGrantsFilters{objectType: "SCHEMA", schema: "serving", object: "events"})},
		{"table_without_object_name", render(objectGrantsFilters{objectType: "TABLE", schema: "serving"})},
		{"template_without_object_name", render(objectGrantsFilters{objectType: "TEMPLATE", schema: "serving"})},
	})
}

// TestObjectGrantsItems checks grouping, skipped rows, column normalization, nullable columns, and stable ordering.
func TestObjectGrantsItems(t *testing.T) {
	rows := []sqlclient.Row{
		{"database_name": "warehouse", "privilege_type": "TEMP", "identity_name": "public", "identity_type": "public", "admin_option": "false", "privilege_scope": "DATABASE"},
		{"database_name": "warehouse", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "admin_option": "f", "privilege_scope": "TABLES"},
		{"database_name": "warehouse", "privilege_type": "USAGE", "identity_name": "IAM:auditor", "identity_type": "global_user", "admin_option": "f", "privilege_scope": "DATABASE"},
		{"database_name": "warehouse", "privilege_type": "CREATE", "identity_name": "analyst", "identity_type": "user", "admin_option": "t", "privilege_scope": "DATABASE"},
	}

	database := objectGrantsItems(objectGrantsFilters{database: "warehouse", objectType: "DATABASE"}, rows)
	if assert.Len(t, database, 2, "scoped grants and unmanaged identity types are skipped") {
		public := database[0]
		assert.Equal(t, types.StringValue("PUBLIC"), public["grantee_type"])
		assert.Equal(t, stringSet("TEMPORARY"), public["privileges"], "catalog abbreviations are normalized")
		assert.Equal(t, types.StringNull(), public["schema_name"])
		assert.Equal(t, types.StringNull(), public["object_name"])
		assert.Equal(t, types.StringValue("DATABASE"), public["object_type"], "the requested kind fills an absent object_type")
		assert.Equal(t, stringSet(), public["grant_option_privileges"])
		assert.Equal(t, stringSet("CREATE"), database[1]["grant_option_privileges"])
	}

	grantee := []sqlclient.Row{
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "INSERT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE"},
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE"},
		{"database_name": "warehouse", "schema_name": "other", "object_name": "t", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE"},
		{"database_name": "warehouse", "schema_name": "serving", "object_type": "SCHEMA", "privilege_type": "USAGE", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "SCHEMA"},
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "f(integer)", "object_type": "FUNCTION", "privilege_type": "EXECUTE", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "FUNCTION"},
		{"database_name": "warehouse", "schema_name": "serving", "object_type": "SCHEMA", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLES"},
	}

	users := objectGrantsItems(objectGrantsFilters{database: "warehouse", grantee: "analyst", granteeType: "USER", schema: "serving"}, grantee)
	if assert.Len(t, users, 2, "routine grants and scoped grants are skipped") {
		assert.Equal(t, types.StringValue("SCHEMA"), users[0]["object_type"])
		assert.Equal(t, types.StringNull(), users[0]["object_name"])
		assert.Equal(t, types.StringValue("events"), users[1]["object_name"])
		assert.Equal(t, stringSet("INSERT", "SELECT"), users[1]["privileges"])
		assert.Equal(t, types.SetNull(types.StringType), users[1]["grant_option_privileges"], "SHOW GRANTS FOR reports no grant options")
	}

	schemaRows := []sqlclient.Row{{"database_name": "warehouse", "schema_name": "serving", "object_name": "serving", "object_type": "SCHEMA", "privilege_type": "USAGE", "identity_name": "readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "SCHEMA"}}

	schemas := objectGrantsItems(objectGrantsFilters{database: "warehouse", objectType: "SCHEMA", schema: "serving", granteeType: "ROLE"}, schemaRows)
	if assert.Len(t, schemas, 1) {
		assert.Equal(t, types.StringValue("serving"), schemas[0]["schema_name"])
		assert.Equal(t, types.StringNull(), schemas[0]["object_name"], "SHOW GRANTS ON SCHEMA reports the schema as object_name")
	}
}
