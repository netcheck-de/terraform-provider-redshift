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

var _ = registerParity(parityCase{source: newGroupsDataSource, resource: newGroupResource, collection: true})

// TestGroupsListing lists groups with their members exactly as the group lookup reports them.
func TestGroupsListing(t *testing.T) {
	c := fullCatalog()
	items, diagnostics := listedItems(t, newGroupsDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assert.Equal(t, "readers", objectString(items[0], "name"))
	assertListedAsLookup(t, items[0], newGroupDataSource, map[string]string{"name": "readers"}, c)
}

// TestGroupsListingFoldsMembers groups the per-member rows of several groups and keeps an empty group's members
// empty rather than null.
func TestGroupsListingFoldsMembers(t *testing.T) {
	client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{
			{"groname": "empty", "grosysid": "100", "usename": ""},
			{"groname": "readers", "grosysid": "101", "usename": "alice"},
			{"groname": "readers", "grosysid": "101", "usename": "bob"},
		}, nil
	})

	items, diagnostics := listedItems(t, newGroupsDataSource, nil, client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 2)
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{}), items[0].Attributes()["members"])
	assert.Equal(t, types.Int64Value(101), items[1].Attributes()["group_id"])
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("alice"), types.StringValue("bob")}), items[1].Attributes()["members"])
}

// TestGroupsListingErrors reports catalog failures and malformed group IDs.
func TestGroupsListingErrors(t *testing.T) {
	malformed := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"groname": "readers", "grosysid": "x"}}, nil
	})

	for name, client := range map[string]sqlclient.Client{"catalog error": failingCatalog, "malformed id": malformed} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := listedItems(t, newGroupsDataSource, nil, client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
