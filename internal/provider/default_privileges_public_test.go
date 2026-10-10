package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultPrivilegesPublicCatalog emulates PG_DEFAULT_ACL for PUBLIC's function defaults: without an entry PUBLIC
// keeps the implicit EXECUTE, and the entry Redshift creates on the first change records whether it was revoked.
type defaultPrivilegesPublicCatalog struct {
	// entry records whether a database-wide function default ACL exists.
	entry bool
	// execute records whether that entry keeps EXECUTE for PUBLIC.
	execute bool
	// writes records the ALTER DEFAULT PRIVILEGES statements.
	writes []string
}

// Query answers the parent checks and the implicit-aware read, and applies GRANT and REVOKE to PUBLIC.
func (c *defaultPrivilegesPublicCatalog) Query(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT privilege_type, admin_option FROM (SELECT"):
		if !c.entry || c.execute {
			return []sqlclient.Row{{"privilege_type": "EXECUTE", "admin_option": "false"}}, nil
		}
		return nil, nil
	case strings.HasPrefix(sql, "SELECT"):
		return []sqlclient.Row{{"name": "exists"}}, nil
	}
	c.writes = append(c.writes, sql)
	// Granting the default back leaves an entry equal to the built-in default, which reads the same as none.
	c.entry, c.execute = true, strings.Contains(sql, " GRANT EXECUTE ")
	return nil, nil
}

// TestDefaultPrivilegesImplicitPublicExecute revokes PUBLIC's implicit EXECUTE on new functions. Deleting the tuple
// never grants it back, so removing the resource cannot widen access; a tuple that kept the default revokes it like
// any owned privilege.
func TestDefaultPrivilegesImplicitPublicExecute(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "owner": "loader", "object_type": "FUNCTIONS", "grantee_type": "PUBLIC", "grantee": "public"}
	c := &defaultPrivilegesPublicCatalog{}
	r := newDefaultPrivilegesResource().(*privilegeResource)
	r.resourceClient = testResourceClient(c)
	data := privilegeObject(t, r, fields)
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"EXECUTE"}, knownStrings(data.Attributes()["privileges"].(types.Set)), "the implicit default is reported")
	require.NoError(t, r.reconcile(context.Background(), privilegeObject(t, r, fields)))
	assert.Equal(t, []string{`ALTER DEFAULT PRIVILEGES FOR USER "loader" REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC`}, c.writes)
	require.False(t, invoke(t, r, "delete", privilegeObject(t, r, fields), false).HasError())
	assert.Len(t, c.writes, 1, "deleting the lockdown leaves it in place")
	c.entry, c.execute = false, false
	require.False(t, invoke(t, r, "delete", privilegeObject(t, r, fields, "EXECUTE"), false).HasError())
	assert.Equal(t, `ALTER DEFAULT PRIVILEGES FOR USER "loader" REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC`, c.writes[len(c.writes)-1])
}

// TestDefaultPrivilegesImplicitScope reads the implicit default only for PUBLIC's database-wide function tuple.
func TestDefaultPrivilegesImplicitScope(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	for _, test := range []struct {
		fields   map[string]string
		implicit bool
	}{
		{map[string]string{"object_type": "FUNCTIONS", "grantee_type": "PUBLIC", "grantee": "public"}, true},
		{map[string]string{"object_type": "FUNCTIONS", "grantee_type": "PUBLIC", "grantee": "public", "owner": ""}, true},
		{map[string]string{"object_type": "FUNCTIONS", "grantee_type": "PUBLIC", "grantee": "public", "schema_name": "serving"}, false},
		{map[string]string{"object_type": "PROCEDURES", "grantee_type": "PUBLIC", "grantee": "public"}, false},
		{map[string]string{"object_type": "FUNCTIONS", "grantee_type": "ROLE", "grantee": "readers"}, false},
	} {
		fields := map[string]string{"database_name": "warehouse", "owner": "loader"}
		for key, value := range test.fields {
			fields[key] = value
		}
		target, err := r.prepare(privilegeObject(t, r, fields))
		require.NoError(t, err)
		assert.Equal(t, test.implicit, strings.Contains(target.query.sql, "pg_default_acl"), "%v", test.fields)
	}
}

// TestDefaultPrivilegesOptionsAndCurrentUser manages a user's grant options on the connecting user's defaults.
func TestDefaultPrivilegesOptionsAndCurrentUser(t *testing.T) {
	exercisePrivilege(t, newDefaultPrivilegesResource, map[string]string{"database_name": "warehouse", "object_type": "TABLES", "grantee_type": "USER", "grantee": "analyst"}, []string{"SELECT", "INSERT"})
	r := newDefaultPrivilegesResource().(*privilegeResource)
	c := optionCatalog(map[string]string{"grantee": "analyst"}, []string{"SELECT"}, nil)
	r.resourceClient = testResourceClient(c)
	fields := map[string]string{"database_name": "warehouse", "object_type": "TABLES", "grantee_type": "USER", "grantee": "analyst"}
	require.NoError(t, r.reconcile(context.Background(), optionObject(t, r, fields, []string{"SELECT"}, []string{"SELECT"})))
	assert.Equal(t, []string{`ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO "analyst" WITH GRANT OPTION`}, c.writes)
}
