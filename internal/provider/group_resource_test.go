package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "group", new: newGroupResource, model: readersGroup(), absent: func(c *catalog) { c.group = false }})

var _ = registerReplacementPolicy("redshift_group", map[string]replaceRule{
	"name": replaceAlways,
})

// readersGroup is a group model whose observed attributes are typed nulls, as before the first read.
func readersGroup() groupModel {
	return groupModel{Name: types.StringValue("readers"), GroupID: types.Int64Null(), Members: types.SetNull(types.StringType)}
}

// TestGroupImport checks restoration of group ownership from JSON.
func TestGroupImport(t *testing.T) {
	r := &groupResource{testResourceClient(&catalog{group: true})}
	state := testState(t, r, readersGroup())
	resp := resource.ImportStateResponse{State: state}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","name":"readers"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError())
}

// TestGroupReadRecordsIDAndMembers checks that refresh reports the group ID and every member row, and that an
// empty group yields an empty member set rather than a member named "".
func TestGroupReadRecordsIDAndMembers(t *testing.T) {
	for name, test := range map[string]struct {
		rows    []sqlclient.Row
		members []attr.Value
	}{
		"members": {
			rows:    []sqlclient.Row{{"groname": "readers", "grosysid": "101", "usename": "alice"}, {"groname": "readers", "grosysid": "101", "usename": "bob"}},
			members: []attr.Value{types.StringValue("alice"), types.StringValue("bob")},
		},
		"empty": {rows: []sqlclient.Row{{"groname": "readers", "grosysid": "101", "usename": ""}}, members: []attr.Value{}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &groupResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return test.rows, nil
			}))}
			data := readersGroup()
			found, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, types.Int64Value(101), data.GroupID)
			assert.Equal(t, types.SetValueMust(types.StringType, test.members), data.Members)
		})
	}
}

// TestGroupReadRejectsInvalidRows reports undecodable IDs and rows for another group instead of guessing.
func TestGroupReadRejectsInvalidRows(t *testing.T) {
	for name, rows := range map[string][]sqlclient.Row{
		"invalid id": {{"groname": "readers", "grosysid": "x", "usename": ""}},
		"ambiguous":  {{"groname": "readers", "grosysid": "101", "usename": "alice"}, {"groname": "Readers", "grosysid": "102", "usename": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &groupResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return rows, nil
			}))}
			data := readersGroup()
			_, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}
