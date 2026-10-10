package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSystemGrantSQL pins the role check, catalog read, and multi-word privilege statements.
func TestSystemGrantSQL(t *testing.T) {
	checkSQL(t, "system_grant", []sqlCase{
		{"create_user", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": "operators"}, "CREATE USER")},
		{"alter_default_privileges", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": "operators"}, "ALTER DEFAULT PRIVILEGES")},
		{"quoted_role", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": `Odd"Role`}, "ACCESS SYSTEM TABLE")},
		{"explain_masking", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": "operators"}, "EXPLAIN MASKING")},
		{"empty_role", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": ""}, "CREATE USER")},
	})
}

// TestSystemGrantAllowlistMatchesReference pins the allowlist to the system permissions of the GRANT role syntax,
// copied in its order. The RBAC system permission table lists the same 34 names. When AWS documents a new
// permission, add it to both lists.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles
// https://docs.aws.amazon.com/redshift/latest/dg/r_roles-system-privileges.html
func TestSystemGrantAllowlistMatchesReference(t *testing.T) {
	documented := []string{
		"CREATE USER", "DROP USER", "ALTER USER",
		"CREATE SCHEMA", "DROP SCHEMA",
		"ALTER DEFAULT PRIVILEGES",
		"ACCESS CATALOG", "ACCESS SYSTEM TABLE",
		"CREATE TABLE", "DROP TABLE", "ALTER TABLE",
		"CREATE OR REPLACE FUNCTION", "CREATE OR REPLACE EXTERNAL FUNCTION",
		"DROP FUNCTION",
		"CREATE OR REPLACE PROCEDURE", "DROP PROCEDURE",
		"CREATE OR REPLACE VIEW", "DROP VIEW",
		"CREATE MODEL", "DROP MODEL",
		"CREATE DATASHARE", "ALTER DATASHARE", "DROP DATASHARE",
		"CREATE LIBRARY", "DROP LIBRARY",
		"CREATE ROLE", "DROP ROLE",
		"TRUNCATE TABLE",
		"VACUUM", "ANALYZE", "CANCEL",
		"IGNORE RLS", "EXPLAIN RLS",
		"EXPLAIN MASKING",
	}
	assert.Len(t, documented, 34)
	assert.Equal(t, documented, privilegeNames(systemPrivileges))
}
