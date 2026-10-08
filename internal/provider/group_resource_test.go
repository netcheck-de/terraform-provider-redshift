package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/require"
)

// TestGroupImport checks restoration of group ownership from JSON.
func TestGroupImport(t *testing.T) {
	r := &groupResource{testResourceClient(&catalog{group: true})}
	state := testState(t, r, groupModel{Name: types.StringValue("readers")})
	resp := resource.ImportStateResponse{State: state}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","name":"readers"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError())
}
