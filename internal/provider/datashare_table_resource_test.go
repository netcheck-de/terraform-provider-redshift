package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatashareTableMembership checks explicit table membership refresh.
func TestDatashareTableMembership(t *testing.T) {
	c := &catalog{shareTable: true}
	r := &datashareTableResource{testResourceClient(c)}
	data := datashareTableModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Table: types.StringValue("table")}
	found, err := r.read(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, found)
	c.shareTable = false
	found, err = r.read(context.Background(), data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestDatashareTableRejectsEmptyNames reports an empty schema or table at plan time and before Create runs SQL,
// because the qualified name would otherwise resolve through the search path.
func TestDatashareTableRejectsEmptyNames(t *testing.T) {
	r := &datashareTableResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))}
	valid := datashareTableModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Table: types.StringValue("table")}
	emptySchema, emptyTable, unknown := valid, valid, valid
	emptySchema.Schema, emptyTable.Table, unknown.Schema = types.StringValue(""), types.StringValue(""), types.StringUnknown()
	for name, test := range map[string]struct {
		model   datashareTableModel
		invalid bool
	}{"valid": {valid, false}, "empty schema": {emptySchema, true}, "empty table": {emptyTable, true}, "unknown schema": {unknown, false}} {
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, test.model))}, &resp)
		assert.Equal(t, test.invalid, resp.Diagnostics.HasError(), "%s: %v", name, resp.Diagnostics)
	}
	plan := testState(t, r, emptySchema)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "a rejected relation must not be recorded in state")
}
