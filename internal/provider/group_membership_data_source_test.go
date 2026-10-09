package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerParity(parityCase{source: newGroupMembershipDataSource, resource: newGroupMembershipResource, selectors: []string{"group", "user"}})

// TestGroupMembershipLookup checks explicit membership presence and absence without group mutations.
func TestGroupMembershipLookup(t *testing.T) {
	exerciseCatalogLookup(t, newGroupMembershipDataSource, map[string]string{"group": "readers", "user": "grafana"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{group: true, groupMember: true})
}
