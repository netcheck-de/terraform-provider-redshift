package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// TestGrantsSQL pins the SHOW GRANTS forms of the grants listing and its rejected filter combinations.
func TestGrantsSQL(t *testing.T) {
	render := func(filters grantsFilters) func() (string, error) {
		return func() (string, error) {
			if filters.database == "" {
				filters.database = "warehouse"
			}
			return readGrantsStatement(filters)
		}
	}
	checkSQL(t, "grants", []sqlCase{
		{"on_database", render(grantsFilters{objectType: "DATABASE"})},
		{"on_schema", render(grantsFilters{objectType: "SCHEMA", schema: "serving"})},
		{"on_table", render(grantsFilters{objectType: "TABLE", schema: "serving", object: "events", grantee: "readers", granteeType: "GROUP"})},
		{"on_table_lower_case_kind", render(grantsFilters{objectType: "table", schema: "serving", object: "events"})},
		{"for_user", render(grantsFilters{grantee: "analyst", granteeType: "USER"})},
		{"for_role", render(grantsFilters{grantee: "example:readers", granteeType: "ROLE", schema: "serving"})},
		{"quoted_identifiers", render(grantsFilters{database: `Odd"Database`, objectType: "TABLE", schema: `Odd"Schema`, object: `Odd"Table`})},
		{"quoted_role", render(grantsFilters{database: `Odd"Database`, grantee: `Odd"O'Reilly\Role`, granteeType: "ROLE"})},
		{"for_group_rejected", render(grantsFilters{grantee: "readers", granteeType: "GROUP"})},
		{"nothing_selected", render(grantsFilters{})},
		{"unsupported_object_type", render(grantsFilters{objectType: "FUNCTION", schema: "serving", object: "f"})},
		{"database_with_schema", render(grantsFilters{objectType: "DATABASE", schema: "serving"})},
		{"schema_without_schema_name", render(grantsFilters{objectType: "SCHEMA"})},
		{"schema_with_object_name", render(grantsFilters{objectType: "SCHEMA", schema: "serving", object: "events"})},
		{"table_without_object_name", render(grantsFilters{objectType: "TABLE", schema: "serving"})},
	})
}

// TestColumnGrantsSQL pins the column privilege listing with and without each optional filter.
func TestColumnGrantsSQL(t *testing.T) {
	render := func(filters grantsFilters) func() (string, error) {
		return func() (string, error) {
			sql, _, err := readColumnGrantsQuery(filters).Build()
			return sql, err
		}
	}
	checkSQL(t, "column_grants", []sqlCase{
		{"unfiltered", render(grantsFilters{})},
		{"all_filters", render(grantsFilters{schema: "serving", object: "events", grantee: "readers", granteeType: "GROUP"})},
		{"grantee_type_only", render(grantsFilters{granteeType: "PUBLIC"})},
	})
}

// TestGrantsItems checks row filtering, column normalization, nullable columns, and stable ordering.
func TestGrantsItems(t *testing.T) {
	rows := []sqlclient.Row{
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "readers", "identity_type": "group", "admin_option": "f", "privilege_scope": "TABLE", "grantor_name": "admin"},
		{"database_name": "warehouse", "privilege_type": "TEMP", "identity_name": "public", "identity_type": "public", "admin_option": "false", "privilege_scope": "DATABASE"},
		{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "INSERT", "identity_name": "analyst", "identity_type": "user", "admin_option": "t", "privilege_scope": "TABLE"},
		{"database_name": "warehouse", "schema_name": "other", "object_name": "t", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE"},
	}
	all := grantsItems(grantsFilters{database: "warehouse", objectType: "DATABASE"}, rows)
	if assert.Len(t, all, 4) {
		first := all[0]
		assert.Equal(t, types.StringValue("TEMPORARY"), first["privilege"], "catalog abbreviations are normalized")
		assert.Equal(t, types.StringNull(), first["schema_name"])
		assert.Equal(t, types.StringValue("DATABASE"), first["object_type"], "the requested kind fills an absent object_type")
		assert.Equal(t, types.StringValue("PUBLIC"), first["grantee_type"])
		assert.Equal(t, types.BoolValue(false), first["admin_option"])
		assert.Equal(t, types.StringNull(), first["grantor"])
	}
	users := grantsItems(grantsFilters{database: "warehouse", grantee: "analyst", granteeType: "USER", schema: "serving"}, rows)
	if assert.Len(t, users, 1) {
		assert.Equal(t, types.StringValue("INSERT"), users[0]["privilege"])
		assert.Equal(t, types.BoolValue(true), users[0]["admin_option"])
	}
	scoped := grantsItems(grantsFilters{database: "warehouse", objectType: "TABLE", schema: "other", object: "t", granteeType: "GROUP"}, rows)
	if assert.Len(t, scoped, 1, "object listings filter only by grantee") {
		assert.Equal(t, types.StringValue("admin"), scoped[0]["grantor"])
	}
	assert.Equal(t, types.BoolNull(), grantsAdminOption(""), "SHOW GRANTS FOR reports no admin_option")
}
