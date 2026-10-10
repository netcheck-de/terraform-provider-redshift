package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

// TestPrivilegeLookupReportsGrantOptions observes the grant option subset on types that manage grant options.
func TestPrivilegeLookupReportsGrantOptions(t *testing.T) {
	source := newPrivilegeDataSource(newOptionGrantTestResource)
	client := optionCatalog(optionGrantFields, []string{"INSERT", "SELECT"}, []string{"SELECT"})
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, optionGrantFields), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, []string{"INSERT", "SELECT"}, knownStrings(observed.Attributes()["privileges"].(types.Set)))
	assert.Equal(t, []string{"SELECT"}, grantOptionPrivileges(observed))
	assert.Empty(t, client.writes)
}

// TestLookupAttributesConvertEveryKind checks the resource-to-lookup conversion for every attribute kind.
func TestLookupAttributesConvertEveryKind(t *testing.T) {
	nested := resourceschema.NestedAttributeObject{Attributes: map[string]resourceschema.Attribute{
		"name":  resourceschema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"size":  resourceschema.Int64Attribute{Optional: true, Computed: true},
		"inner": resourceschema.ListNestedAttribute{Optional: true, NestedObject: resourceschema.NestedAttributeObject{Attributes: map[string]resourceschema.Attribute{"flag": resourceschema.BoolAttribute{Optional: true}}}},
	}}
	source := map[string]resourceschema.Attribute{
		"selector":         resourceschema.StringAttribute{Required: true, MarkdownDescription: "Selected name. Changing it replaces the object.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"filter":           resourceschema.BoolAttribute{Optional: true},
		"text":             resourceschema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Text; changing it replaces the object."},
		"owner":            resourceschema.StringAttribute{Optional: true, Computed: true},
		"count":            resourceschema.Int64Attribute{Optional: true},
		"count32":          resourceschema.Int32Attribute{Optional: true},
		"ratio":            resourceschema.Float64Attribute{Optional: true},
		"ratio32":          resourceschema.Float32Attribute{Optional: true},
		"number":           resourceschema.NumberAttribute{Optional: true},
		"dynamic":          resourceschema.DynamicAttribute{Optional: true},
		"list":             resourceschema.ListAttribute{Optional: true, ElementType: types.StringType},
		"privileges":       resourceschema.SetAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Exact privileges."},
		"map":              resourceschema.MapAttribute{Optional: true, ElementType: types.Int64Type},
		"object":           resourceschema.ObjectAttribute{Optional: true, AttributeTypes: map[string]attr.Type{"a": types.StringType}},
		"list_nested":      resourceschema.ListNestedAttribute{Optional: true, NestedObject: nested},
		"set_nested":       resourceschema.SetNestedAttribute{Optional: true, NestedObject: nested},
		"map_nested":       resourceschema.MapNestedAttribute{Optional: true, NestedObject: nested},
		"single":           resourceschema.SingleNestedAttribute{Optional: true, Attributes: nested.Attributes},
		"secret_wo":        resourceschema.StringAttribute{Optional: true, WriteOnly: true},
		"refresh_revision": resourceschema.StringAttribute{Optional: true},
	}
	converted := lookupAttributes(source, false, []string{"text", "count", "count32", "ratio", "ratio32", "number", "dynamic", "list", "privileges", "map", "object", "list_nested", "set_nested", "map_nested", "single"})
	assert.NotContains(t, converted, "secret_wo", "write-only inputs cannot be observed")
	assert.NotContains(t, converted, "refresh_revision", "apply-time triggers cannot be observed")
	require.Len(t, converted, len(source)-2)
	for name, attribute := range converted {
		assert.Equal(t, source[name].GetType(), attribute.GetType(), name)
	}
	selector := converted["selector"].(schema.StringAttribute)
	assert.True(t, selector.Required)
	assert.False(t, selector.Computed)
	assert.Len(t, selector.Validators, 1, "inputs keep their validators")
	assert.Equal(t, "Selected name.", selector.MarkdownDescription)
	filter := converted["filter"].(schema.BoolAttribute)
	assert.True(t, filter.Optional)
	assert.False(t, filter.Computed)
	text := converted["text"].(schema.StringAttribute)
	assert.True(t, text.Computed)
	assert.True(t, text.Sensitive)
	assert.Equal(t, "Text.", text.MarkdownDescription)
	assert.True(t, converted["owner"].IsComputed(), "computed resource attributes are observed")
	assert.Equal(t, privilegesLookupDescription, converted["privileges"].GetMarkdownDescription())
	for name, attribute := range converted {
		if name == "selector" || name == "filter" {
			continue
		}
		assert.True(t, attribute.IsComputed(), name)
		assert.False(t, attribute.IsRequired(), name)
		assert.False(t, attribute.IsOptional(), name)
	}
	var computedThroughout func(name string, attributes map[string]schema.Attribute)
	computedThroughout = func(name string, attributes map[string]schema.Attribute) {
		for child, attribute := range attributes {
			assert.True(t, attribute.IsComputed(), "%s.%s", name, child)
			assert.False(t, attribute.IsRequired() || attribute.IsOptional(), "%s.%s", name, child)
			if inner, ok := attribute.(schema.ListNestedAttribute); ok {
				computedThroughout(name+"."+child, inner.NestedObject.Attributes)
			}
		}
	}
	computedThroughout("list_nested", converted["list_nested"].(schema.ListNestedAttribute).NestedObject.Attributes)
	computedThroughout("set_nested", converted["set_nested"].(schema.SetNestedAttribute).NestedObject.Attributes)
	computedThroughout("map_nested", converted["map_nested"].(schema.MapNestedAttribute).NestedObject.Attributes)
	computedThroughout("single", converted["single"].(schema.SingleNestedAttribute).Attributes)
	// A nested selector keeps its children's flags and validators.
	input := lookupAttributes(map[string]resourceschema.Attribute{"list_nested": source["list_nested"]}, false, nil)["list_nested"].(schema.ListNestedAttribute)
	assert.True(t, input.Optional)
	name := input.NestedObject.Attributes["name"].(schema.StringAttribute)
	assert.True(t, name.Required)
	assert.Len(t, name.Validators, 1)
	assert.True(t, input.NestedObject.Attributes["size"].IsComputed())
	// Every converted kind accepts a lookup description, at the top level and inside each nested kind.
	paths := map[string]string{}
	for name := range converted {
		paths[name] = "Observed " + name + "."
	}
	for _, nested := range []string{"list_nested", "set_nested", "map_nested", "single"} {
		paths[nested+".name"] = "Observed " + nested + " name."
	}
	described := lookupDescriptions(&catalogDataSource{attributes: converted}, paths).(*catalogDataSource)
	descriptions := nestedDescriptions(described.attributes)
	for path, description := range paths {
		assert.Equal(t, description, descriptions[path], path)
	}
}

// blockTestLookup observes the block test resource with a lookup-only selector, filling one column so a block's
// computed counterpart is shown to round-trip through data-source state.
func blockTestLookup() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "block_test", factory: newBlockTestResource, identityFields: []string{"name"}, identityDatabase: "database",
		selectors: map[string]schema.Attribute{"column_types": schema.ListAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Column types selecting the table."}},
		lookup: func(ctx context.Context, _ *resourceClient, data *types.Object) (bool, error) {
			columnType := data.AttributeTypes(ctx)["column"].(types.ListType).ElemType.(types.ObjectType)
			identityType := columnType.AttrTypes["identity"].(types.ObjectType)
			column := types.ObjectValueMust(columnType.AttrTypes, map[string]attr.Value{
				"name": types.StringValue("event_id"), "type": types.StringValue("bigint"), "encoding": types.StringValue("az64"),
				"identity": types.ObjectValueMust(identityType.AttrTypes, map[string]attr.Value{"seed": types.Int64Value(1), "step": types.Int64Value(1)}),
			})
			lookupValue(data, "column", types.ListValueMust(columnType, []attr.Value{column}))
			return true, nil
		},
	})
}

// nestedDescriptions maps every attribute's dotted path to its description, descending into nested attributes.
func nestedDescriptions(attributes map[string]schema.Attribute) map[string]string {
	descriptions := map[string]string{}
	for name, attribute := range attributes {
		descriptions[name] = attribute.GetMarkdownDescription()
		var nested map[string]schema.Attribute
		switch attribute := attribute.(type) {
		case schema.ListNestedAttribute:
			nested = attribute.NestedObject.Attributes
		case schema.SetNestedAttribute:
			nested = attribute.NestedObject.Attributes
		case schema.MapNestedAttribute:
			nested = attribute.NestedObject.Attributes
		case schema.SingleNestedAttribute:
			nested = attribute.Attributes
		}
		for path, description := range nestedDescriptions(nested) {
			descriptions[name+"."+path] = description
		}
	}
	return descriptions
}

// unsupportedBlock is a block kind the lookup conversion does not know.
type unsupportedBlock struct {
	// ListNestedBlock supplies the Block interface without matching its case.
	resourceschema.ListNestedBlock
}

// TestLookupBlocks converts every block shape into computed nested attributes of the same type, less write-only
// attributes, drops the required-block note, and adds lookup-only selectors.
func TestLookupBlocks(t *testing.T) {
	var source resource.SchemaResponse
	newBlockTestResource().Schema(context.Background(), resource.SchemaRequest{}, &source)
	converted := lookupSchemaAttributes(source.Schema.Attributes, source.Schema.Blocks, false, nil)
	require.Len(t, converted, len(source.Schema.Attributes)+len(source.Schema.Blocks))
	for _, name := range []string{"unique", "distribution"} {
		assert.Equal(t, source.Schema.Blocks[name].Type(), converted[name].GetType(), name)
	}
	assert.True(t, converted["database"].IsRequired(), "attributes keep their selector flags")
	column := converted["column"].(schema.ListNestedAttribute)
	assert.NotContains(t, column.NestedObject.Attributes, "default_wo", "a write-only block attribute cannot be observed")
	assert.Equal(t, "Columns in order.", column.MarkdownDescription)
	assert.IsType(t, schema.SetNestedAttribute{}, converted["unique"])
	assert.IsType(t, schema.SingleNestedAttribute{}, converted["distribution"])
	identity := column.NestedObject.Attributes["identity"].(schema.SingleNestedAttribute)
	for path, attribute := range map[string]schema.Attribute{
		"column": column, "column.name": column.NestedObject.Attributes["name"], "column.encoding": column.NestedObject.Attributes["encoding"],
		"column.identity": identity, "column.identity.seed": identity.Attributes["seed"],
		"distribution.style": converted["distribution"].(schema.SingleNestedAttribute).Attributes["style"],
	} {
		assert.True(t, attribute.IsComputed(), path)
		assert.False(t, attribute.IsRequired() || attribute.IsOptional(), path)
	}
	assert.Equal(t, "Unique constraints, changed in place.", lookupDescription("Unique constraints, changed in place."))
	assert.Panics(t, func() { lookupBlocks(map[string]resourceschema.Block{"other": unsupportedBlock{}}) })

	lookup := dataSourceSchema(blockTestLookup)
	assert.Empty(t, lookup.Blocks)
	assert.True(t, lookup.Attributes["column_types"].IsOptional())
	for _, shadow := range []string{"column", "name", "id", "exists"} {
		assert.Panics(t, func() {
			newCatalogDataSource(catalogSpec{name: "block_test", factory: newBlockTestResource, exists: true, selectors: map[string]schema.Attribute{shadow: schema.StringAttribute{Optional: true}}})
		}, "selector %s shadows a lookup output", shadow)
	}
	for _, computed := range []string{"column", "missing"} {
		assert.Panics(t, func() {
			newCatalogDataSource(catalogSpec{name: "block_test", factory: newBlockTestResource, computed: []string{computed}})
		}, "computed %s is not a resource attribute", computed)
	}
	assert.NotPanics(t, func() {
		newCatalogDataSource(catalogSpec{name: "block_test", factory: newBlockTestResource, exists: true, computed: []string{"owner"}, selectors: map[string]schema.Attribute{"column_types": schema.StringAttribute{Optional: true}}})
	})
}

// TestLookupBlocksRead fills a block's computed counterpart and leaves unobserved blocks null.
func TestLookupBlocksRead(t *testing.T) {
	source := blockTestLookup()
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, map[string]string{"database": "analytics", "name": "events"}), queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("unexpected query " + sql)
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	columns := observed.Attributes()["column"].(types.List).Elements()
	require.Len(t, columns, 1)
	column := columns[0].(types.Object).Attributes()
	assert.Equal(t, types.StringValue("event_id"), column["name"])
	assert.Equal(t, types.Int64Value(1), column["identity"].(types.Object).Attributes()["seed"])
	assert.True(t, observed.Attributes()["unique"].IsNull())
	assert.True(t, observed.Attributes()["distribution"].IsNull())
	assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "analytics", map[string]string{"name": "events"})
}

// TestLookupDescriptions replaces top-level and nested descriptions and rejects paths that do not exist.
func TestLookupDescriptions(t *testing.T) {
	source := lookupDescriptions(blockTestLookup(), map[string]string{
		"owner": "Observed owner.", "column": "Observed columns.", "column.identity.seed": "Observed seed.", "distribution.style": "Observed style.",
	})
	var response datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	descriptions := nestedDescriptions(response.Schema.Attributes)
	assert.Equal(t, "Observed owner.", descriptions["owner"])
	assert.Equal(t, "Observed columns.", descriptions["column"])
	assert.Equal(t, "Observed seed.", descriptions["column.identity.seed"])
	assert.Equal(t, "Observed style.", descriptions["distribution.style"])
	assert.Equal(t, "Column name.", descriptions["column.name"], "unnamed nested outputs keep the resource text")
	assert.Equal(t, "Increment.", descriptions["column.identity.step"])
	for _, path := range []string{"missing", "column.missing", "database.child", "column.identity.seed.child"} {
		assert.Panics(t, func() { lookupDescriptions(blockTestLookup(), map[string]string{path: "text"}) }, path)
	}
}
