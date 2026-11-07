package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRolesDataSource, resource: newRoleResource, collection: true})

// TestRolesListing lists roles with their owner, external ID, and ID exactly as the role lookup reports them.
func TestRolesListing(t *testing.T) {
	c := fullCatalog()
	fakeState[*roleFakeFamily](c, "role").externalID = "ABC123"

	items, diagnostics := listedItems(t, newRolesDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assert.Equal(t, "example:readers", objectString(items[0], "name"))
	assert.Equal(t, types.Int64Value(100), items[0].Attributes()["role_id"])
	assertListedAsLookup(t, items[0], newRoleDataSource, map[string]string{"name": "example:readers"}, c)

	c.role = false
	items, diagnostics = listedItems(t, newRolesDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, items, "no role is an empty listing, not an error")
}

// TestRolesListingErrors reports catalog failures and malformed role IDs.
func TestRolesListingErrors(t *testing.T) {
	malformed := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"role_id": "x", "role_name": "readers"}}, nil
	})

	for name, client := range map[string]sqlclient.Client{"catalog error": failingCatalog, "malformed id": malformed} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := listedItems(t, newRolesDataSource, nil, client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
