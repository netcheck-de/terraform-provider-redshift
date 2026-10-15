package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newGroupDataSource, resource: newGroupResource, selectors: []string{"name"}})

// TestGroupDataSourceLooksUpExistingGroup verifies the group lookup with its ID and members.
func TestGroupDataSourceLooksUpExistingGroup(t *testing.T) {
	for name, test := range map[string]struct {
		member  bool
		members []string
	}{"with member": {true, []string{"grafana"}}, "empty": {false, []string{}}} {
		t.Run(name, func(t *testing.T) {
			state, diagnostics := readSource(t, newGroupDataSource(), groupData{Name: types.StringValue("readers")}, &catalog{group: true, groupMember: test.member})
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			var data groupData
			require.False(t, state.Get(context.Background(), &data).HasError())
			assert.Equal(t, "readers", data.Name.ValueString())
			assert.Equal(t, int64(101), data.GroupID.ValueInt64())
			assert.Equal(t, test.members, data.Members)
			assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "readers"})
		})
	}
}

// TestGroupDataSourceReportsMissingGroup checks the diagnostic for an absent group.
func TestGroupDataSourceReportsMissingGroup(t *testing.T) {
	_, diagnostics := readSource(t, newGroupDataSource(), groupData{Name: types.StringValue("readers")}, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, nil
	}))
	require.True(t, diagnostics.HasError())
}
