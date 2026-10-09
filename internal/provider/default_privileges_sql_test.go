package provider

import (
	"maps"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestDefaultPrivilegesSQL pins the catalog read and ALTER DEFAULT PRIVILEGES statements for every object kind,
// with and without a schema.
func TestDefaultPrivilegesSQL(t *testing.T) {
	tables := map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee_type": "ROLE", "grantee": "readers"}
	render := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(tables)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newDefaultPrivilegesResource, fields, privilege)
	}
	checkSQL(t, "default_privileges", []sqlCase{
		{"tables_in_schema_role", render(nil, "SELECT")},
		{"tables_database_wide", render(map[string]string{"schema_name": ""}, "REFERENCES")},
		{"functions_user", render(map[string]string{"object_type": "FUNCTIONS", "grantee_type": "USER", "grantee": "analyst"}, "EXECUTE")},
		{"procedures_public", render(map[string]string{"schema_name": "", "object_type": "PROCEDURES", "grantee_type": "PUBLIC", "grantee": "public"}, "EXECUTE")},
		{"tables_group", render(map[string]string{"grantee_type": "GROUP"}, "TRUNCATE")},
		{"quoted_identifiers", render(map[string]string{"database_name": `Odd"Database`, "owner": `Odd"Owner`, "schema_name": `Odd"Schema`, "grantee": `Odd"Role`}, "SELECT")},
		{"unsupported_object_type", render(map[string]string{"object_type": "SEQUENCES"}, "SELECT")},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"}, "SELECT")},
		{"empty_owner", render(map[string]string{"owner": ""}, "SELECT")},
	})
}
