package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

// TestGroupMembershipImport checks restoration of a group/user relationship.
func TestGroupMembershipImport(t *testing.T) {
	r := &groupMembershipResource{testResourceClient(&catalog{group: true, groupMember: true})}
	state := testState(t, r, groupMembershipModel{Group: types.StringValue("readers"), User: types.StringValue("grafana")})
	resp := resource.ImportStateResponse{State: state}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","group":"readers","user":"grafana"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError())
}
