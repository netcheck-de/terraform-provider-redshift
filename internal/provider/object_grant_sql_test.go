package provider

import (
	"maps"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestObjectGrantSQL pins the checks, SHOW GRANTS read, and GRANT/REVOKE statements of every object kind.
func TestObjectGrantSQL(t *testing.T) {
	table := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "orders", "object_type": "TABLE", "grantee_type": "GROUP", "grantee": "readers"}
	render := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(table)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newObjectGrantResource, fields, privilege)
	}
	checkSQL(t, "object_grant", []sqlCase{
		{"table_group", render(nil, "SELECT")},
		{"table_public", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, "TRUNCATE")},
		{"schema_user", render(map[string]string{"object_type": "SCHEMA", "object_name": "", "grantee_type": "USER", "grantee": "loader"}, "USAGE")},
		{"database_role", render(map[string]string{"object_type": "DATABASE", "schema_name": "", "object_name": "", "grantee_type": "ROLE", "grantee": "readers"}, "TEMPORARY")},
		{"quoted_identifiers", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_name": `Odd"Table`, "grantee_type": "ROLE", "grantee": `Odd"Role`}, "SELECT")},
		{"database_with_schema", render(map[string]string{"object_type": "DATABASE", "object_name": ""}, "TEMPORARY")},
		{"schema_without_schema_name", render(map[string]string{"object_type": "SCHEMA", "schema_name": "", "object_name": ""}, "USAGE")},
		{"schema_with_object_name", render(map[string]string{"object_type": "SCHEMA"}, "USAGE")},
		{"table_without_object_name", render(map[string]string{"object_name": ""}, "SELECT")},
		{"unsupported_object_type", render(map[string]string{"object_type": "VIEW"}, "SELECT")},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"}, "SELECT")},
		{"empty_database_name", render(map[string]string{"object_type": "DATABASE", "database_name": "", "schema_name": "", "object_name": ""}, "TEMPORARY")},
	})
}
