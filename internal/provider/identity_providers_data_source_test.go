package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newIdentityProvidersDataSource, resource: newIdentityProviderResource, collection: true})

// TestIdentityProvidersListing lists AWSIDC and Azure providers exactly as the identity provider lookup reports them,
// without the secret or the automatic role creation settings.
func TestIdentityProvidersListing(t *testing.T) {
	c := fullCatalog()
	items, diagnostics := listedItems(t, newIdentityProvidersDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assert.Equal(t, "AWSIDC", objectString(items[0], "type"))
	assert.Equal(t, "role-one", objectString(items[0], "iam_role_arn"))

	for _, name := range []string{"client_secret_wo", "client_secret_wo_version", "auto_create_roles"} {
		assert.NotContains(t, items[0].Attributes(), name)
	}

	assertListedAsLookup(t, items[0], newIdentityProviderDataSource, map[string]string{"name": "identity"}, c)

	family := fakeState[*identityProviderFakeFamily](c, "identity_provider")
	family.azure = true
	family.parameters = `{"issuer":"https://login.microsoftonline.com/tenant/v2.0", "client_id":"client", "client_secret":, "audience":["b", "a"]}`
	items, diagnostics = listedItems(t, newIdentityProvidersDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assert.Equal(t, "AZURE", objectString(items[0], "type"))
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringValue("b")}), items[0].Attributes()["audience"])
	assertListedAsLookup(t, items[0], newIdentityProviderDataSource, map[string]string{"name": "identity"}, c)
}

// TestIdentityProvidersListingErrors reports catalog failures and providers the lookup also rejects.
func TestIdentityProvidersListingErrors(t *testing.T) {
	row := func(kind string) sqlclient.Client {
		return queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"name": "okta", "type": kind, "instanceid": "i", "namespc": "ns", "params": "{}", "enabled": "true"}}, nil
		})
	}

	for name, client := range map[string]sqlclient.Client{"catalog error": failingCatalog, "unsupported type": row("okta"), "missing role": row("awsidc")} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := listedItems(t, newIdentityProvidersDataSource, nil, client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
