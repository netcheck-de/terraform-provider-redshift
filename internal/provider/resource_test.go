package provider

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryFunc adapts a callback into a SQL client for targeted resource tests.
type queryFunc func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error)

// Query adapts a test callback to the transport-neutral SQL client interface.
func (f queryFunc) Query(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	return f(ctx, target, sql, parameters)
}

// testResourceClient supplies a known warehouse/admin binding around a fake SQL client.
func testResourceClient(client sqlclient.Client) resourceClient {
	return resourceClient{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}
}

// testState serializes a resource model with its actual Terraform schema. A model without the resource's timeouts
// block, such as the definition a resource shares with its lookup, leaves the block absent, as a configuration
// without it does.
func testState(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()

	ctx := context.Background()

	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)

	return testStateWithSchema(t, response.Schema, model)
}

// testStateWithSchema lets a test reuse its resource schema without bypassing timeout serialization.
func testStateWithSchema(t *testing.T, schema schema.Schema, model any) tfsdk.State {
	t.Helper()

	ctx := context.Background()
	state := tfsdk.State{Schema: schema}

	block, hasTimeouts := schema.Blocks[timeoutsBlockName]
	if !hasTimeouts || modelHasField(model, timeoutsBlockName) {
		diagnostics := state.Set(ctx, model)
		require.False(t, diagnostics.HasError(), "invalid test state: %v", diagnostics)

		return state
	}

	without := schema
	without.Blocks = maps.Clone(schema.Blocks)
	delete(without.Blocks, timeoutsBlockName)
	partial := tfsdk.State{Schema: without}
	diagnostics := partial.Set(ctx, model)
	require.False(t, diagnostics.HasError(), "invalid test state: %v", diagnostics)

	values := map[string]tftypes.Value{}
	require.NoError(t, partial.Raw.As(&values))
	values[timeoutsBlockName] = tftypes.NewValue(block.Type().TerraformType(ctx), nil)
	state.Raw = tftypes.NewValue(schema.Type().TerraformType(ctx), values)

	return state
}

// modelHasField reports whether a struct model, or the struct a pointer model points to, declares or promotes a field
// tagged tfsdk:"name".
func modelHasField(model any, name string) bool {
	value := reflect.Indirect(reflect.ValueOf(model))
	if value.Kind() != reflect.Struct {
		return false
	}

	for _, field := range reflect.VisibleFields(value.Type()) {
		if field.Tag.Get("tfsdk") == name {
			return true
		}
	}

	return false
}

// TestImportRejectsInvalidIdentity checks malformed and incomplete JSON import IDs.
func TestImportRejectsInvalidIdentity(t *testing.T) {
	r := &roleResource{}

	for _, id := range []string{"invalid JSON", `[]`, `null`, `{}`, `{"name":""}`, `{"workgroup_name":"warehouse","database":"admin"}`, `{"workgroup_name":"warehouse","name":"role"}`, `{"endpoint":"","name":"role"}`} {
		t.Run(id, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: emptyState(t, r)}
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
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
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
	r := testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, nil
	}))
	require.ErrorContains(t, r.bound(types.StringValue("invalid JSON"), "admin"), "invalid resource identity")
	require.ErrorContains(t, r.bound(types.StringValue("null"), "admin"), "invalid resource identity")
	require.ErrorContains(t, r.bound(types.StringValue("{}"), "admin"), "invalid resource identity")
	require.ErrorContains(t, r.bound(types.StringValue(`{"workgroup_name":"warehouse","database":"other"}`), "admin"), "differs")
	require.ErrorContains(t, r.bound(types.StringValue(`{"database":"other","name":"table"}`), "admin"), "differs")
	require.NoError(t, r.bound(types.StringNull(), "admin"))
	require.NoError(t, r.bound(types.StringUnknown(), "admin"))
}

// TestIdentityPreservesTuple checks that encoding neither injects scope nor mutates shared identity fields.
func TestIdentityPreservesTuple(t *testing.T) {
	for name, fields := range map[string]map[string]string{
		"warehouse": {"name": "readers"},
		"database":  {"database": "analytics", "schema": "serving", "name": "events"},
	} {
		t.Run(name, func(t *testing.T) {
			before := maps.Clone(fields)
			id := identity(fields)

			var decoded map[string]string
			require.NoError(t, json.Unmarshal([]byte(id.ValueString()), &decoded))
			assert.Equal(t, before, decoded)
			assert.Equal(t, before, fields)
			assert.Equal(t, id, identity(fields))
		})
	}
}

// TestIdentityIsProviderRelative checks that connection configuration never enters new IDs.
func TestIdentityIsProviderRelative(t *testing.T) {
	r := testResourceClient(queryFunc(func(_ context.Context, target sqlclient.Connection, _ string, _ map[string]string) ([]sqlclient.Row, error) {
		assert.Equal(t, "admin", target.Database)
		return nil, nil
	}))
	legacy := types.StringValue(`{"workgroup_name":"warehouse","database":"admin","name":"readers"}`)
	id := identity(map[string]string{"name": "readers"})
	assert.JSONEq(t, `{"name":"readers"}`, id.ValueString())
	require.NoError(t, r.bound(legacy, "admin"))
	assert.Equal(t, id, normalizeLegacyIdentity(legacy, false))

	r.warehouse = warehouseBinding{field: "endpoint", value: types.StringValue("example:5439")}

	assert.Equal(t, id, identity(map[string]string{"name": "readers"}))
	require.NoError(t, r.bound(id, "admin"))
	require.ErrorContains(t, r.bound(legacy, "admin"), "differs")
	_, err := r.query(context.Background(), "SELECT 1", nil)
	require.NoError(t, err)
}

// TestImportRejectsConnectionSelectors separates legacy state support from the current import format.
func TestImportRejectsConnectionSelectors(t *testing.T) {
	for _, field := range warehouseIdentityFields {
		client := testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, nil
		}))
		client.warehouse = warehouseBinding{field: field, value: types.StringValue("warehouse")}
		r := &roleResource{client}
		encoded, err := json.Marshal(map[string]string{field: "warehouse", "database": "admin", "name": "readers"})
		require.NoError(t, err)

		id := types.StringValue(string(encoded))
		resp := resource.ImportStateResponse{State: testState(t, r, roleModel{Name: types.StringValue("readers")})}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id.ValueString()}, &resp)
		require.True(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), field)
		require.NoError(t, client.bound(id, "admin"))
	}

	r := &roleResource{}
	resp := resource.ImportStateResponse{State: testState(t, r, roleModel{})}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","cluster_identifier":"cluster","database":"admin","name":"readers"}`}, &resp)
	require.True(t, resp.Diagnostics.HasError())
}

// TestImportRejectsAdministrationDatabase prevents imports from carrying redundant connection context.
func TestImportRejectsAdministrationDatabase(t *testing.T) {
	r := &roleResource{}
	resp := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"database":"admin","name":"readers"}`}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "administration database")
}

// TestMinimalImportIdentity restores a warehouse-wide object without connection or administration context.
func TestMinimalImportIdentity(t *testing.T) {
	r := &roleResource{testResourceClient(nil)}
	resp := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"name":"readers"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)

	var data roleModel
	require.False(t, resp.State.Get(context.Background(), &data).HasError())
	assert.Equal(t, "readers", data.Name.ValueString())
	assert.JSONEq(t, `{"name":"readers"}`, data.ID.ValueString())
}
