package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newIdentityProviderDataSource, resource: newIdentityProviderResource, selectors: []string{"name"}})

// TestIdentityProviderLookup checks read-only AWSIDC integration attributes and catalog details.
func TestIdentityProviderLookup(t *testing.T) {
	c := &catalog{identity: true, iamRole: "role-one", enabled: true}
	state, diagnostics := readSource(t, newIdentityProviderDataSource(), identityProviderData{Name: types.StringValue("identity")}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data identityProviderData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, identityProviderAWSIDC, data.Type.ValueString())
	assert.Equal(t, "example", data.Namespace.ValueString())
	assert.Equal(t, "application", data.ApplicationARN.ValueString())
	assert.Equal(t, "role-one", data.IAMRoleARN.ValueString())
	assert.True(t, data.Enabled.ValueBool())
	assert.Equal(t, int64(126692), data.ProviderID.ValueInt64())
	assert.Equal(t, "arn:aws:sso:::instance/ssoins-1234567890abcdef", data.IdentityCenterInstanceARN.ValueString())
	assert.True(t, data.AutoCreateRoles.IsNull(), "role creation is not in the catalog")
	assert.Empty(t, c.writes, "lookups never write")
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "identity"})
}

// TestIdentityProviderLookupAzure checks the Azure parameters and that the lookup schema omits the secret.
func TestIdentityProviderLookupAzure(t *testing.T) {
	state, diagnostics := readSource(t, newIdentityProviderDataSource(), identityProviderData{Name: types.StringValue("oauth_standard")}, identityProviderAzureCatalog(true))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data identityProviderData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, identityProviderAzure, data.Type.ValueString())
	assert.Equal(t, "https://sts.windows.net/tenant/", data.Issuer.ValueString())
	assert.Equal(t, "87f4aa26-78b7-410e-bf29-57b39929ef9a", data.ClientID.ValueString())
	assert.Equal(t, []string{"https://analysis.windows.net/powerbi/connector/AmazonRedshift"}, data.Audience)
	assert.True(t, data.ApplicationARN.IsNull())
	schema := dataSourceSchema(newIdentityProviderDataSource)
	assert.NotContains(t, schema.Attributes, "client_secret_wo")
	assert.NotContains(t, schema.Attributes, "client_secret_wo_version")
}
