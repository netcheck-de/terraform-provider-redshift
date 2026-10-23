package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_default_privileges", map[string]replaceRule{
	"restore_default_on_destroy": replaceNever,
	"database_name":              replaceAlways,
	"schema_name":                replaceAlways,
	"owner":                      replaceAlways,
	"object_type":                replaceAlways,
	"grantee":                    replaceAlways,
	"grantee_type":               replaceAlways,
	"privileges":                 replaceNever,
	"grant_option_privileges":    replaceNever,
})

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

// TestDefaultPrivilegesAllowlist lists the privileges each object type accepts and defers an unknown type.
func TestDefaultPrivilegesAllowlist(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	for kind, count := range map[string]int{"TABLES": 7, "FUNCTIONS": 1, "PROCEDURES": 1, "VIEWS": 0} {
		t.Run(kind, func(t *testing.T) {
			data := privilegeObject(t, r, map[string]string{"object_type": kind})
			allowed, err := defaultPrivilegesConfigAllowed(data)
			if count == 0 {
				require.ErrorContains(t, err, "unsupported default object_type")
				return
			}
			require.NoError(t, err)
			assert.Len(t, allowed, count)
		})
	}
	data := privilegeObject(t, r, nil)
	attributes := data.Attributes()
	attributes["object_type"] = types.StringUnknown()
	allowed, err := defaultPrivilegesConfigAllowed(types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes))
	require.NoError(t, err)
	assert.Nil(t, allowed)
}
