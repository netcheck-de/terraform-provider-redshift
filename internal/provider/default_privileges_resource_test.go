package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultPrivilegesLifecycle exercises schema-specific creator default permissions.
func TestDefaultPrivilegesLifecycle(t *testing.T) {
	exercisePrivilege(t, newDefaultPrivilegesResource, map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee_type": "ROLE", "grantee": "readers"}, []string{"SELECT", "INSERT"})
}

// TestDefaultPrivilegesObjectTypes checks routine/table defaults and global parameter handling.
func TestDefaultPrivilegesObjectTypes(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	for _, kind := range []string{"FUNCTIONS", "PROCEDURES", "TABLES", "invalid"} {
		target, err := r.prepare(privilegeObject(t, r, map[string]string{"database_name": "warehouse", "owner": "loader", "object_type": kind, "grantee_type": "ROLE", "grantee": "readers"}))
		if kind == "invalid" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.NotContains(t, target.query.parameters, "schema", "Data API parameters cannot contain empty strings")
		}
	}
	_, err := r.prepare(privilegeObject(t, r, map[string]string{"grantee_type": "invalid"}))
	require.Error(t, err)
}
