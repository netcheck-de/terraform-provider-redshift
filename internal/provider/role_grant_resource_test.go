package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoleGrantObservesSystemRole checks a built-in role granted to another role.
func TestRoleGrantObservesSystemRole(t *testing.T) {
	c := &catalog{role: true, membership: true}
	r := &roleGrantResource{testResourceClient(c)}
	data := roleGrantModel{
		Role: types.StringValue("sys:dba"), ToRole: types.StringValue("example:readers"), ToUser: types.StringNull(),
	}
	found, err := r.read(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, found)
	c.membership = false
	found, err = r.read(context.Background(), data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRoleGrantObservesUserMembership checks role-to-user catalog membership.
func TestRoleGrantObservesUserMembership(t *testing.T) {
	c := &catalog{user: true, userGrant: true}
	r := &roleGrantResource{testResourceClient(c)}
	data := roleGrantModel{Role: types.StringValue("sys:monitor"), ToRole: types.StringNull(), ToUser: types.StringValue("grafana")}
	found, err := r.read(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, found)
	c.userGrant = false
	found, err = r.read(context.Background(), data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRoleGrantRejectsAmbiguousImport rejects missing or competing recipient identities.
func TestRoleGrantRejectsAmbiguousImport(t *testing.T) {
	r := &roleGrantResource{}
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	for _, id := range []string{"invalid", `{"workgroup_name":"warehouse","database":"admin","role":"sys:monitor"}`, `{"workgroup_name":"warehouse","database":"admin","role":"sys:monitor","to_user":"grafana","to_role":"readers"}`} {
		resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.True(t, resp.Diagnostics.HasError())
	}
}
