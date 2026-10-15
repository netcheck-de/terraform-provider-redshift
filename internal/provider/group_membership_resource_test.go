package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "group membership", kind: lifecyclePermission, new: newGroupMembershipResource, model: groupMembershipModel{Group: types.StringValue("readers"), User: types.StringValue("grafana")}, absent: func(c *catalog) { c.groupMember = false }})

var _ = registerReplacementPolicy("redshift_group_membership", map[string]replaceRule{
	"group": replaceAlways,
	"user":  replaceAlways,
})

// TestGroupMembershipImport checks restoration of a group/user relationship.
func TestGroupMembershipImport(t *testing.T) {
	r := &groupMembershipResource{testResourceClient(&catalog{group: true, groupMember: true})}
	state := testState(t, r, groupMembershipModel{Group: types.StringValue("readers"), User: types.StringValue("grafana")})
	resp := resource.ImportStateResponse{State: state}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","group":"readers","user":"grafana"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError())
}
