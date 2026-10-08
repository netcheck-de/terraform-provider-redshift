package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertLookupIdentity checks resource identity parity and the exact persisted keys independently.
func assertLookupIdentity(t *testing.T, id types.String, database string, fields map[string]string) {
	t.Helper()
	require.False(t, id.IsNull())
	require.False(t, id.IsUnknown())
	expected := map[string]string{}
	for key, value := range fields {
		expected[key] = value
	}
	client := resourceClient{warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}}
	assert.Equal(t, client.identity(database, expected), id)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(id.ValueString()), &decoded))
	assert.Equal(t, expected, decoded)
}

// catalogLookupObject builds a typed lookup configuration with null computed fields.
func catalogLookupObject(t *testing.T, source datasource.DataSource, fields map[string]string) types.Object {
	t.Helper()
	var schema datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
	attributes, attributeTypes := map[string]attr.Value{}, map[string]attr.Type{}
	for name, attribute := range schema.Schema.Attributes {
		attributeType := attribute.GetType()
		attributeTypes[name] = attributeType
		value, err := attributeType.ValueFromTerraform(context.Background(), tftypes.NewValue(attributeType.TerraformType(context.Background()), nil))
		require.NoError(t, err)
		attributes[name] = value
		if input, ok := fields[name]; ok {
			attributes[name] = types.StringValue(input)
		}
	}
	return types.ObjectValueMust(attributeTypes, attributes)
}

// exerciseCatalogLookup checks observations, absence, SQL errors, malformed configuration, and read-only execution.
func exerciseCatalogLookup(t *testing.T, factory func() datasource.DataSource, fields map[string]string, expected map[string]attr.Value, client sqlclient.Client) {
	t.Helper()
	source := factory()
	var metadata datasource.MetadataResponse
	source.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	assert.True(t, strings.HasPrefix(metadata.TypeName, "redshift_"))
	data := catalogLookupObject(t, source, fields)
	state, diagnostics := readSource(t, source, data, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		require.True(t, strings.HasPrefix(sql, "SELECT ") || strings.HasPrefix(sql, "SHOW "), "lookup must not mutate: %s", sql)
		return client.Query(ctx, target, sql, parameters)
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	for name, value := range expected {
		assert.True(t, value.Equal(observed.Attributes()[name]), "%s: expected %v, got %v", name, value, observed.Attributes()[name])
	}
	database := "admin"
	if value, ok := fields["database"]; ok {
		database = value
	}
	assertLookupIdentity(t, observed.Attributes()["id"].(types.String), database, fields)
	// A computed identity from an earlier observation must not prevent a fresh lookup or survive absence.
	lookupValue(&data, "id", types.StringValue("stale observation"))
	for _, unavailable := range []bool{false, true} {
		state, diagnostics := readSource(t, factory(), data, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			if unavailable {
				return nil, errors.New("catalog unavailable")
			}
			return nil, nil
		}))
		_, relationship := expected["exists"]
		assert.Equal(t, unavailable || !relationship, diagnostics.HasError(), "%v", diagnostics)
		if !diagnostics.HasError() {
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Equal(t, types.BoolValue(false), observed.Attributes()["exists"])
			assert.Equal(t, types.StringNull(), observed.Attributes()["id"])
			if _, schemaMembership := expected["include_new"]; schemaMembership {
				assert.Equal(t, types.BoolValue(false), observed.Attributes()["include_new"])
			}
		}
	}
	var schema datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	source.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: tftypes.NewValue(tftypes.String, "invalid")}}, &resp)
	require.True(t, resp.Diagnostics.HasError())
}

// TestPrivilegeLookupsReturnEmptySetsAndGrantOptions checks absent grants and observational reads of unmanaged permissions.
func TestPrivilegeLookupsReturnEmptySetsAndGrantOptions(t *testing.T) {
	fields := map[string]string{"role": "readers"}
	for _, values := range []map[string]bool{{}, {"CREATE USER": true, "NEW CAPABILITY": true}} {
		client := &privilegeCatalog{values: values, admin: "true"}
		source := newSystemGrantDataSource()
		state, diagnostics := readSource(t, source, catalogLookupObject(t, source, fields), client)
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var observed types.Object
		require.False(t, state.Get(context.Background(), &observed).HasError())
		assert.Len(t, observed.Attributes()["privileges"].(types.Set).Elements(), len(values))
		assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", fields)
		assert.Empty(t, client.writes)
	}
}
