package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newGroupMembershipsDataSource, resource: newGroupMembershipResource, collection: true, filters: []string{"group", "user"}})

// TestGroupMembershipsListing lists memberships, narrowed by group and user, and reports catalog errors.
func TestGroupMembershipsListing(t *testing.T) {
	for _, filters := range []map[string]string{{}, {"group": "readers"}, {"user": "grafana"}, {"group": "readers", "user": "grafana"}} {
		items, diagnostics := listedItems(t, newGroupMembershipsDataSource, filters, fullCatalog())
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		require.Len(t, items, 1, "%v", filters)
		assert.Equal(t, "readers", objectString(items[0], "group"))
		assert.Equal(t, "grafana", objectString(items[0], "user"))
	}

	for _, filters := range []map[string]string{{"group": "writers"}, {"user": "other"}} {
		items, diagnostics := listedItems(t, newGroupMembershipsDataSource, filters, fullCatalog())
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		assert.Empty(t, items, "%v", filters)
	}

	_, diagnostics := listedItems(t, newGroupMembershipsDataSource, nil, failingCatalog)
	assert.True(t, diagnostics.HasError())
}
