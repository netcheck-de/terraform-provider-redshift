package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSchemaListing is a tiny collection over schema names, owners, and a numeric column, so the helper is
// tested without a production listing.
func testSchemaListing() datasource.DataSource {
	element := collectionElement(newSchemaResource, "database", "name", "owner")
	element["tables"] = schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of tables."}
	return newCollectionDataSource(collectionSpec{
		name:        "test_schemas",
		description: "Lists schemas.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database to list; defaults to the provider database."},
			"owner":    schema.StringAttribute{Optional: true, MarkdownDescription: "Only schemas owned by this user."},
		},
		element: element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := client.database.ValueString()
			if selected := objectString(filters, "database"); selected != "" {
				database = selected
			}
			query := sqlclient.Select("nspname", "usename").From("pg_namespace").OptEq("usename", "owner", objectString(filters, "owner"))
			rows, err := client.selectRows(ctx, database, query)
			if err != nil {
				return nil, err
			}
			var items []map[string]attr.Value
			for _, row := range rows {
				items = append(items, map[string]attr.Value{"database": types.StringValue(database), "name": types.StringValue(row["nspname"]), "owner": types.StringValue(row["usename"]), "tables": types.Int64Value(2)})
			}
			return items, nil
		},
	})
}

// collectionConfig builds a listing configuration with the given string filters and null computed values.
func collectionConfig(t *testing.T, source datasource.DataSource, filters map[string]string) types.Object {
	t.Helper()
	var response datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	attributes, attributeTypes := map[string]attr.Value{}, map[string]attr.Type{}
	for name, attribute := range response.Schema.Attributes {
		attributeType := attribute.GetType()
		attributeTypes[name] = attributeType
		value, err := attributeType.ValueFromTerraform(context.Background(), tftypes.NewValue(attributeType.TerraformType(context.Background()), nil))
		require.NoError(t, err)
		attributes[name] = value
		if filter, ok := filters[name]; ok {
			attributes[name] = types.StringValue(filter)
		}
	}
	return types.ObjectValueMust(attributeTypes, attributes)
}

// TestCollectionDataSourceSchema checks the filter, identity, and element shape shared by all listings.
func TestCollectionDataSourceSchema(t *testing.T) {
	source := testSchemaListing()
	var metadata datasource.MetadataResponse
	source.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	assert.Equal(t, "redshift_test_schemas", metadata.TypeName)
	assertCollectionParity(t, parityCase{source: testSchemaListing, filters: []string{"database", "owner"}, collection: true})
	resourceShaped := func() datasource.DataSource {
		return newCollectionDataSource(collectionSpec{name: "test_schemas", element: collectionElement(newSchemaResource, "database", "name", "owner"), filters: map[string]schema.Attribute{"owner": schema.StringAttribute{Optional: true}}})
	}
	assertCollectionParity(t, parityCase{source: resourceShaped, resource: newSchemaResource, filters: []string{"owner"}, collection: true})
	assert.Panics(t, func() { collectionElement(newSchemaResource, "missing") })
	var response datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	assert.Contains(t, response.Schema.Attributes, "test_schemas", "the result is named after the data source")
	assert.NotContains(t, response.Schema.Attributes, "items")
	for _, reserved := range []string{"test_schemas", "id"} {
		assert.Panics(t, func() {
			newCollectionDataSource(collectionSpec{name: "test_schemas", filters: map[string]schema.Attribute{reserved: schema.StringAttribute{Optional: true}}})
		}, "a filter named %s would be overwritten by the read", reserved)
	}
}

// collectionResult returns a listing's result list, which is named after the data source.
func collectionResult(source datasource.DataSource, observed types.Object) types.List {
	return observed.Attributes()[source.(*collectionDataSource).spec.name].(types.List)
}

// TestCollectionElementFromBlocks lists resource blocks as computed nested attributes of the same shape.
func TestCollectionElementFromBlocks(t *testing.T) {
	element := collectionElement(newBlockTestResource, "database", "name", "column", "unique", "distribution")
	listing := func() datasource.DataSource {
		return newCollectionDataSource(collectionSpec{name: "block_tests", element: element})
	}
	assertCollectionParity(t, parityCase{source: listing, resource: newBlockTestResource, collection: true})
	column := element["column"].(schema.ListNestedAttribute)
	assert.True(t, column.Computed)
	assert.IsType(t, schema.SingleNestedAttribute{}, column.NestedObject.Attributes["identity"])
}

// TestCollectionDataSourceRead checks filtered and unfiltered listings, empty results, and failures.
func TestCollectionDataSourceRead(t *testing.T) {
	var target sqlclient.Connection
	var parameters map[string]string
	var rows []sqlclient.Row
	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "SELECT nspname, usename FROM pg_namespace")
		target, parameters = connection, params
		return rows, nil
	})
	for _, test := range []struct {
		name     string
		filters  map[string]string
		rows     []sqlclient.Row
		database string
		identity map[string]string
	}{
		{"unfiltered", nil, []sqlclient.Row{{"nspname": "public", "usename": "admin"}, {"nspname": "serving", "usename": "etl"}}, "admin", map[string]string{"workgroup_name": "warehouse", "database": "admin"}},
		{"filtered", map[string]string{"database": "analytics", "owner": "etl"}, []sqlclient.Row{{"nspname": "serving", "usename": "etl"}}, "analytics", map[string]string{"workgroup_name": "warehouse", "database": "analytics", "owner": "etl"}},
		{"empty", map[string]string{"owner": "nobody"}, nil, "admin", map[string]string{"workgroup_name": "warehouse", "database": "admin", "owner": "nobody"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows = test.rows
			source := testSchemaListing()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.database, target.Database)
			if owner, ok := test.filters["owner"]; ok {
				assert.Equal(t, map[string]string{"owner": owner}, parameters)
			}
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			var identity map[string]string
			require.NoError(t, json.Unmarshal([]byte(observed.Attributes()["id"].(types.String).ValueString()), &identity))
			assert.Equal(t, test.identity, identity)
			items := collectionResult(source, observed)
			require.False(t, items.IsNull(), "an empty listing is an empty list, not null")
			require.Len(t, items.Elements(), len(test.rows))
			for index, row := range test.rows {
				item := items.Elements()[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(row["nspname"]), item["name"])
				assert.Equal(t, types.StringValue(row["usename"]), item["owner"])
				assert.Equal(t, types.StringValue(test.database), item["database"])
				assert.Equal(t, types.Int64Value(2), item["tables"])
			}
		})
	}
	source := testSchemaListing()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError(), "catalog failures are errors")
	malformed := newCollectionDataSource(collectionSpec{name: "test_schemas", element: map[string]schema.Attribute{"name": schema.StringAttribute{Computed: true}}, list: func(context.Context, *resourceClient, types.Object) ([]map[string]attr.Value, error) {
		return []map[string]attr.Value{{"unknown": types.StringValue("x")}}, nil
	}})
	_, diagnostics = readSource(t, malformed, collectionConfig(t, malformed, nil), client)
	assert.True(t, diagnostics.HasError(), "elements must match the element type")
	var response datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: response.Schema}}
	source.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: response.Schema, Raw: tftypes.NewValue(tftypes.String, "invalid")}}, &resp)
	assert.True(t, resp.Diagnostics.HasError())
}
