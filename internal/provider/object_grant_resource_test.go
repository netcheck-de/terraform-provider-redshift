package provider

import (
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_object_grant", map[string]replaceRule{
	"database_name": replaceAlways,
	"schema_name":   replaceAlways,
	"object_name":   replaceAlways,
	"object_type":   replaceAlways,
	"grantee":       replaceAlways,
	"grantee_type":  replaceAlways,
	"privileges":    replaceNever,
})

// TestObjectGrantLifecycle exercises explicit group privileges on a local table.
func TestObjectGrantLifecycle(t *testing.T) {
	exercisePrivilege(t, newObjectGrantResource, map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "table", "object_type": "TABLE", "grantee_type": "GROUP", "grantee": "readers"}, []string{"SELECT", "INSERT"})
}

// TestObjectGrantScopeValidation rejects incompatible object/schema/name combinations.
func TestObjectGrantScopeValidation(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	for _, test := range []struct {
		kind, schema, name string
		valid              bool
	}{
		{"DATABASE", "", "", true}, {"DATABASE", "serving", "", false}, {"SCHEMA", "serving", "", true}, {"SCHEMA", "", "", false}, {"SCHEMA", "serving", "table", false}, {"TABLE", "serving", "", false}, {"invalid", "", "", false},
	} {
		target, err := r.prepare(privilegeObject(t, r, map[string]string{"database_name": "warehouse", "schema_name": test.schema, "object_name": test.name, "object_type": test.kind, "grantee_type": "USER", "grantee": "reader"}))
		assert.Equal(t, test.valid, err == nil)
		if test.valid {
			assert.False(t, target.filter(sqlclient.Row{"identity_name": "other"}))
		}
	}
	_, err := r.prepare(privilegeObject(t, r, map[string]string{"grantee_type": "invalid"}))
	require.Error(t, err)
}
