package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageCatalogInputKinds(t *testing.T) {
	attributes := []resourceschema.Attribute{
		resourceschema.Int64Attribute{Required: true}, resourceschema.Int32Attribute{Required: true}, resourceschema.Float32Attribute{Required: true}, resourceschema.DynamicAttribute{Required: true}, resourceschema.Float64Attribute{Required: true}, resourceschema.NumberAttribute{Required: true},
		resourceschema.MapAttribute{Required: true, ElementType: types.StringType}, resourceschema.ObjectAttribute{Required: true, AttributeTypes: map[string]attr.Type{"name": types.StringType}},
		resourceschema.SetNestedAttribute{Required: true, NestedObject: resourceschema.NestedAttributeObject{Attributes: map[string]resourceschema.Attribute{"name": resourceschema.StringAttribute{Required: true}}}},
		resourceschema.MapNestedAttribute{Required: true, NestedObject: resourceschema.NestedAttributeObject{Attributes: map[string]resourceschema.Attribute{"name": resourceschema.StringAttribute{Required: true}}}},
		resourceschema.SingleNestedAttribute{Required: true, Attributes: map[string]resourceschema.Attribute{"name": resourceschema.StringAttribute{Required: true}}},
	}
	for _, attribute := range attributes {
		converted := lookupAttribute(attribute, false, "selector")
		assert.True(t, converted.IsRequired())
		assert.False(t, converted.IsComputed())
		assert.Equal(t, "selector", converted.GetMarkdownDescription())
	}
}

type coverageExistsResource struct{ resource.Resource }

func (coverageExistsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = resourceschema.Schema{Attributes: map[string]resourceschema.Attribute{"exists": resourceschema.BoolAttribute{Computed: true}}}
}

func TestCoverageCatalogExistsCollision(t *testing.T) {
	assert.PanicsWithValue(t, "newCatalogDataSource: the exists output shadows a resource attribute or block", func() {
		newCatalogDataSource(catalogSpec{exists: true, factory: func() resource.Resource { return coverageExistsResource{} }})
	})
}

func TestCoverageCatalogCollectionBooleanIdentity(t *testing.T) {
	d := newCollectionDataSource(collectionSpec{name: "items", filters: map[string]schema.Attribute{"enabled": schema.BoolAttribute{Optional: true}}}).(*collectionDataSource)
	d.resourceClient = testResourceClient(fullCatalog())
	data := types.ObjectValueMust(map[string]attr.Type{"enabled": types.BoolType}, map[string]attr.Value{"enabled": types.BoolValue(true)})
	assertLookupIdentity(t, d.collectionID(data), "admin", map[string]string{"enabled": "true"})
}

func TestCoverageCatalogConfigurationMalformed(t *testing.T) {
	d := newConfigurationDataSource()
	var source datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &source)
	var response datasource.ReadResponse
	d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: source.Schema, Raw: tftypes.NewValue(tftypes.String, "invalid")}}, &response)
	require.True(t, response.Diagnostics.HasError())
}

func TestCoverageCatalogConnectionTimeout(t *testing.T) {
	data := passwordProvider()
	data.Connection.ConnectTimeout = types.StringValue("not a duration")
	_, err := data.transport("test")
	require.ErrorContains(t, err, "direct_connection.connect_timeout")
}
