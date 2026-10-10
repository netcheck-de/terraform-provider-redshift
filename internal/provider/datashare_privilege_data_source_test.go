package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatasharePrivilegeDataSource, resource: newDatasharePrivilegeResource, selectors: []string{"database_name", "datashare_name", "grantee", "grantee_type"}})

// TestDatasharePrivilegeLookup observes one role's explicit datashare permissions without reconciling them.
func TestDatasharePrivilegeLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDatasharePrivilegeDataSource, datasharePrivilegeFields, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SHARE")})}, &privilegeCatalog{values: map[string]bool{"SHARE": true}, grantee: "share_admins", kind: "role"})
}

// TestDatasharePrivilegeLookupReportsGrantOptions reads permissions held with grant option, which the resource
// refuses to manage, because a lookup only observes.
func TestDatasharePrivilegeLookupReportsGrantOptions(t *testing.T) {
	c := fullCatalog()
	fakeState[*fakeDatashares](c, "datashare").adminOption = true
	source := newDatasharePrivilegeDataSource()
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, datashareFullFields), c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("ALTER")}), observed.Attributes()["privileges"])
	assert.Empty(t, c.writes)
}
