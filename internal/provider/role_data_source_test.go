package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRoleDataSource, resource: newRoleResource, selectors: []string{"name"}})

// TestRoleLookup checks the read-only role lookup, including its owner, external ID, and ID.
func TestRoleLookup(t *testing.T) {
	c := &catalog{role: true}
	fakeState[*roleFakeFamily](c, "role").externalID = "ABC123"
	state, diagnostics := readSource(t, newRoleDataSource(), roleData{Name: types.StringValue("example:readers")}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data roleData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "example:readers", data.Name.ValueString())
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, "ABC123", data.ExternalID.ValueString())
	assert.Equal(t, int64(100), data.RoleID.ValueInt64())
	assert.Empty(t, c.writes, "lookups never write")
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "example:readers"})
}
