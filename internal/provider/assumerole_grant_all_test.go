package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssumeroleGrantAllRoles manages commands on every IAM role, which the catalog reports under the default role.
func TestAssumeroleGrantAllRoles(t *testing.T) {
	exercisePrivilege(t, newAssumeroleGrantResource, map[string]string{"iam_role_arn": "ALL", "grantee_type": "USER", "grantee": "analyst"}, []string{"UNLOAD", "COPY"})
	r := newAssumeroleGrantResource().(*privilegeResource)
	target, err := r.prepare(privilegeObject(t, r, map[string]string{"iam_role_arn": "ALL", "grantee_type": "USER", "grantee": "analyst"}))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"arn": assumeroleGrantDefaultRole, "grantee": "analyst", "kind": "USER"}, target.query.parameters)
}

// TestAssumeroleGrantPublicSwitch revokes PUBLIC's default ASSUMEROLE ON ALL FOR ALL, as Redshift requires before
// per-identity grants take effect. Deleting the tuple never grants the default back: destroying or replacing it must
// not let every user assume every IAM role.
func TestAssumeroleGrantPublicSwitch(t *testing.T) {
	fields := map[string]string{"iam_role_arn": "ALL", "grantee_type": "PUBLIC", "grantee": "public"}
	r := newAssumeroleGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{"COPY": true, "UNLOAD": true, "EXTERNAL FUNCTION": true, "CREATE MODEL": true}}
	r.resourceClient = testResourceClient(c)
	require.NoError(t, r.reconcile(context.Background(), privilegeObject(t, r, fields)))
	assert.Equal(t, []string{
		"REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR COPY",
		"REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR CREATE MODEL",
		"REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR EXTERNAL FUNCTION",
		"REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR UNLOAD",
	}, c.writes)
	c.writes = nil
	require.False(t, invoke(t, r, "delete", privilegeObject(t, r, fields), false).HasError())
	assert.Empty(t, c.writes, "deleting the revoked switch leaves access control on")
	c.values["COPY"] = true
	require.False(t, invoke(t, r, "delete", privilegeObject(t, r, fields, "COPY"), false).HasError())
	assert.Equal(t, []string{"REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR COPY"}, c.writes, "deleting revokes what the tuple owns")
}
