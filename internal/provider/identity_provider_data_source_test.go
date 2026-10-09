package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newIdentityProviderDataSource, resource: newIdentityProviderResource, selectors: []string{"name"}})

// TestIdentityProviderLookup checks read-only AWSIDC integration attributes.
func TestIdentityProviderLookup(t *testing.T) {
	state, diagnostics := readSource(t, newIdentityProviderDataSource(), identityProviderData{Name: types.StringValue("identity")}, &catalog{identity: true, iamRole: "role-one", enabled: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data identityProviderData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "example", data.Namespace.ValueString())
	assert.Equal(t, "application", data.ApplicationARN.ValueString())
	assert.Equal(t, "role-one", data.IAMRoleARN.ValueString())
	assert.True(t, data.Enabled.ValueBool())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "identity"})
}
