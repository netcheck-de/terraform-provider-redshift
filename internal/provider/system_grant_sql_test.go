package provider

import (
	"testing"
)

// TestSystemGrantSQL pins the role check, catalog read, and multi-word privilege statements.
func TestSystemGrantSQL(t *testing.T) {
	checkSQL(t, "system_grant", []sqlCase{
		{"create_user", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": "operators"}, "CREATE USER")},
		{"alter_default_privileges", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": "operators"}, "ALTER DEFAULT PRIVILEGES")},
		{"quoted_role", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": `Odd"Role`}, "ACCESS SYSTEM TABLE")},
		{"empty_role", privilegeTargetCase(t, newSystemGrantResource, map[string]string{"role": ""}, "CREATE USER")},
	})
}
