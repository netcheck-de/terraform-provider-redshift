package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestRoleGrantLookup checks both recipient kinds and absence without granting roles.
func TestRoleGrantLookup(t *testing.T) {
	for _, recipient := range []string{"to_role", "to_user"} {
		exerciseCatalogLookup(t, newRoleGrantDataSource, map[string]string{"role": "sys:dba", recipient: "example:readers"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{role: true, membership: true, user: true, userGrant: true})
	}
}
