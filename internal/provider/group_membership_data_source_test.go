package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestGroupMembershipLookup checks explicit membership presence and absence without group mutations.
func TestGroupMembershipLookup(t *testing.T) {
	exerciseCatalogLookup(t, newGroupMembershipDataSource, map[string]string{"group": "readers", "user": "grafana"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{group: true, groupMember: true})
}
