package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGroupDataSourceLooksUpExistingGroup verifies group lookup without membership ownership.
func TestGroupDataSourceLooksUpExistingGroup(t *testing.T) {
	state, diagnostics := readSource(t, newGroupDataSource(), groupData{Name: types.StringValue("readers")}, &catalog{group: true})
	require.False(t, diagnostics.HasError())
	var data groupData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "readers", data.Name.ValueString())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "readers"})
}

// TestGroupDataSourceReportsMissingGroup checks the diagnostic for an absent group.
func TestGroupDataSourceReportsMissingGroup(t *testing.T) {
	_, diagnostics := readSource(t, newGroupDataSource(), groupData{Name: types.StringValue("readers")}, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, nil
	}))
	require.True(t, diagnostics.HasError())
}
