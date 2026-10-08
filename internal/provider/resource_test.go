package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryFunc adapts a callback into a SQL client for targeted resource tests.
type queryFunc func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error)

// Query adapts a test callback to the transport-neutral SQL client interface.
func (f queryFunc) Query(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
	return f(ctx, target, sql, parameters)
}

// testResourceClient supplies a known warehouse/admin binding around a fake SQL client.
func testResourceClient(client dataapi.Client) resourceClient {
	return resourceClient{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}
}

// testState serializes a resource model with its actual Terraform schema.
func testState(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	diagnostics := state.Set(context.Background(), model)
	require.False(t, diagnostics.HasError(), "invalid test state: %v", diagnostics)
	return state
}

// TestImportRejectsInvalidIdentity checks malformed and incomplete JSON import IDs.
func TestImportRejectsInvalidIdentity(t *testing.T) {
	r := &roleResource{}
	for _, id := range []string{"invalid JSON", `[]`, `null`, `{}`, `{"name":"role"}`, `{"workgroup_name":"warehouse","database":"admin"}`} {
		t.Run(id, func(t *testing.T) {
			var schema resource.SchemaResponse
			r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
			resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
			assert.True(t, resp.Diagnostics.HasError(), "expected invalid import diagnostics")
		})
	}
}

// TestResourceClientConfiguration checks client injection and known connection requirements.
func TestResourceClientConfiguration(t *testing.T) {
	r := &resourceClient{}
	var resp resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{}, &resp)
	assert.False(t, resp.Diagnostics.HasError())
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: 42}, &resp)
	assert.True(t, resp.Diagnostics.HasError())
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return nil, nil
	}), warehouse: warehouseBinding{field: "workgroup_name", value: types.StringUnknown()}, database: types.StringValue("admin")}}, &resource.ConfigureResponse{})
	_, err := r.query(context.Background(), "SELECT 1", nil)
	require.Error(t, err)
	r.warehouse.value = types.StringValue("warehouse")
	for _, database := range []string{"", "admin"} {
		_, err := r.queryDatabase(context.Background(), database, "SELECT 1", nil)
		if database == "" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

// TestBindingRejectsInvalidIdentity checks invalid JSON and cross-database state adoption.
func TestBindingRejectsInvalidIdentity(t *testing.T) {
	r := testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return nil, nil
	}))
	require.ErrorContains(t, r.bound(types.StringValue("invalid JSON"), "admin"), "invalid resource identity")
	require.ErrorContains(t, r.bound(types.StringValue(`{"workgroup_name":"warehouse","database":"other"}`), "admin"), "differs")
	require.NoError(t, r.bound(types.StringNull(), "admin"))
}

// TestWarehouseBindingIsIndependentOfSQLTransport checks generic bindings and legacy ID compatibility.
func TestWarehouseBindingIsIndependentOfSQLTransport(t *testing.T) {
	r := testResourceClient(queryFunc(func(_ context.Context, target dataapi.Connection, _ string, _ map[string]string) ([]dataapi.Row, error) {
		assert.Equal(t, "admin", target.Database)
		return nil, nil
	}))
	legacy := r.identity("admin", map[string]string{"name": "readers"})
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","name":"readers"}`, legacy.ValueString())
	r.warehouse = warehouseBinding{field: "endpoint", value: types.StringValue("example:5439")}
	id := r.identity("admin", map[string]string{"name": "readers"})
	assert.JSONEq(t, `{"endpoint":"example:5439","database":"admin","name":"readers"}`, id.ValueString())
	require.NoError(t, r.bound(id, "admin"))
	require.ErrorContains(t, r.bound(legacy, "admin"), "differs")
	_, err := r.query(context.Background(), "SELECT 1", nil)
	require.NoError(t, err)
}

// TestImportSupportsClusterAndEndpointBindings preserves transport-independent ownership and rejects mixed identities.
func TestImportSupportsClusterAndEndpointBindings(t *testing.T) {
	for _, field := range []string{"cluster_identifier", "endpoint"} {
		client := testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return nil, nil
		}))
		client.warehouse = warehouseBinding{field: field, value: types.StringValue("warehouse")}
		r := &roleResource{client}
		id := client.identity("admin", map[string]string{"name": "readers"})
		resp := resource.ImportStateResponse{State: testState(t, r, roleModel{Name: types.StringValue("readers")})}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id.ValueString()}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		require.NoError(t, client.bound(id, "admin"))
		other := testResourceClient(nil)
		require.ErrorContains(t, other.bound(id, "admin"), "differs")
	}
	r := &roleResource{}
	resp := resource.ImportStateResponse{State: testState(t, r, roleModel{})}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","cluster_identifier":"cluster","database":"admin","name":"readers"}`}, &resp)
	require.True(t, resp.Diagnostics.HasError())
}
