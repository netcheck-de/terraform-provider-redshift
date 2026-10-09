package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRoleDataSource, resource: newRoleResource, selectors: []string{"name"}})

// TestRoleLookup checks read-only role existence lookup.
func TestRoleLookup(t *testing.T) {
	state, diagnostics := readSource(t, newRoleDataSource(), roleData{Name: types.StringValue("example:readers")}, &catalog{role: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data roleData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "example:readers", data.Name.ValueString())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "example:readers"})
}
