package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRoleGrantsDataSource, resource: newRoleGrantResource, collection: true, filters: []string{"role", "to_user", "to_role"}})

// roleGrantKeys returns the listed grants as role>recipient strings, with the admin option of user grants.
func roleGrantKeys(items []types.Object) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		recipient := "user:" + objectString(item, "to_user")
		if objectString(item, "to_user") == "" {
			recipient = "role:" + objectString(item, "to_role")
		}

		admin := ""
		if item.Attributes()["admin_option"].(types.Bool).ValueBool() {
			admin = "+admin"
		}

		keys = append(keys, objectString(item, "role")+">"+recipient+admin)
	}

	return keys
}

// TestRoleGrantsListing lists grants to users and roles, narrowed by role and recipient, with the admin option.
func TestRoleGrantsListing(t *testing.T) {
	c := fullCatalog()
	c.user, c.userGrant = true, true
	fakeState[*roleGrantFakeFamily](c, "role_grant").admin = true

	for _, test := range []struct {
		filters  map[string]string
		expected []string
	}{
		{nil, []string{"sys:monitor>user:grafana+admin", "sys:dba>role:example:readers"}},
		{map[string]string{"role": "sys:dba"}, []string{"sys:dba>role:example:readers"}},
		{map[string]string{"to_user": "grafana"}, []string{"sys:monitor>user:grafana+admin"}},
		{map[string]string{"to_role": "example:readers"}, []string{"sys:dba>role:example:readers"}},
	} {
		items, diagnostics := listedItems(t, newRoleGrantsDataSource, test.filters, c)
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		assert.Equal(t, test.expected, roleGrantKeys(items), "%v", test.filters)
	}

	items, diagnostics := listedItems(t, newRoleGrantsDataSource, map[string]string{"to_user": "grafana"}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assertListedAsLookup(t, items[0], newRoleGrantDataSource, map[string]string{"role": "sys:monitor", "to_user": "grafana"}, c)
}

// TestRoleGrantsListingFoldsGrantors reports one grant per role and user, with the admin option when any grantor
// gave it, and reports catalog errors and malformed admin options.
func TestRoleGrantsListingFoldsGrantors(t *testing.T) {
	rows := []sqlclient.Row{
		{"role_name": "readers", "user_name": "alice", "admin_option": "false"},
		{"role_name": "readers", "user_name": "alice", "admin_option": "true"},
		{"role_name": "readers", "user_name": "bob", "admin_option": "false"},
	}
	client := func(rows []sqlclient.Row) sqlclient.Client {
		return queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if strings.Contains(sql, "FROM svv_user_grants") {
				return rows, nil
			}

			return nil, nil
		})
	}

	items, diagnostics := listedItems(t, newRoleGrantsDataSource, nil, client(rows))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{"readers>user:alice+admin", "readers>user:bob"}, roleGrantKeys(items))

	for name, client := range map[string]sqlclient.Client{
		"catalog error":   failingCatalog,
		"malformed admin": client([]sqlclient.Row{{"role_name": "readers", "user_name": "alice", "admin_option": "x"}}),
	} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := listedItems(t, newRoleGrantsDataSource, nil, client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
