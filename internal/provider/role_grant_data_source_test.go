package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerParity(parityCase{source: newRoleGrantDataSource, resource: newRoleGrantResource, selectors: []string{"role", "to_user", "to_role"}})

// TestRoleGrantLookup checks both recipient kinds, the user's admin option, and absence without granting roles.
func TestRoleGrantLookup(t *testing.T) {
	for _, recipient := range []string{"to_role", "to_user"} {
		c := &catalog{role: true, membership: true, user: true, userGrant: true}
		fakeState[*roleGrantFakeFamily](c, "role_grant").admin = true
		// A role recipient never holds the admin option, which only svv_user_grants reports.
		admin := recipient == "to_user"
		exerciseCatalogLookup(t, newRoleGrantDataSource, map[string]string{"role": "sys:dba", recipient: "example:readers"}, map[string]attr.Value{"exists": types.BoolValue(true), "admin_option": types.BoolValue(admin)}, c)
	}
}
