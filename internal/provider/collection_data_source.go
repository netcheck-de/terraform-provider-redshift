package provider

import (
	"context"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// collectionItems is the computed attribute holding a collection's elements.
const collectionItems = "items"

// collectionSpec describes a read-only listing of catalog objects.
type collectionSpec struct {
	// name is the Terraform data-source suffix.
	name string
	// description is the data source's documentation.
	description string
	// filters are optional inputs narrowing the listing; a filter named database also selects the database
	// recorded in the identity.
	filters map[string]schema.Attribute
	// element defines the computed attributes of each listed object.
	element map[string]schema.Attribute
	// list returns the matching objects, keyed by element attribute, without executing mutations. Filters
	// arrive as the configured data-source object, so unset filters are null.
	list func(context.Context, *resourceClient, types.Object) ([]map[string]attr.Value, error)
}

// collectionDataSource lists catalog objects, where an empty result is an answer rather than an error.
type collectionDataSource struct {
	// dataSourceClient provides transport-neutral catalog execution.
	dataSourceClient
	// spec is the listing's schema and catalog read.
	spec collectionSpec
}

// newCollectionDataSource builds a listing data source from its spec.
func newCollectionDataSource(spec collectionSpec) datasource.DataSource {
	return &collectionDataSource{spec: spec}
}

// collectionElement derives listed attributes from a resource's schema, all computed, so element types
// cannot drift from the resource they describe.
func collectionElement(factory func() resource.Resource, names ...string) map[string]schema.Attribute {
	var source resource.SchemaResponse
	factory().Schema(context.Background(), resource.SchemaRequest{}, &source)
	observed := lookupAttributes(source.Schema.Attributes, true, nil)
	element := map[string]schema.Attribute{}
	for _, name := range names {
		attribute, ok := observed[name]
		if !ok {
			panic("collectionElement: resource has no readable attribute " + name)
		}
		element[name] = attribute
	}
	return element
}

// elementType returns the object type of one listed element.
func (d *collectionDataSource) elementType() types.ObjectType {
	return schema.NestedAttributeObject{Attributes: d.spec.element}.Type().(types.ObjectType)
}

// Metadata identifies the concrete listing to Terraform.
func (d *collectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.name
}

// Schema exposes the optional filters, the listing identity, and the computed elements.
func (d *collectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := maps.Clone(d.spec.filters)
	if attributes == nil {
		attributes = map[string]schema.Attribute{}
	}
	attributes["id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "JSON identity of this listing: the warehouse binding, the database, and the configured filters."}
	attributes[collectionItems] = schema.ListNestedAttribute{
		Computed: true, MarkdownDescription: "Matching objects; empty when nothing matches.",
		NestedObject: schema.NestedAttributeObject{Attributes: d.spec.element},
	}
	resp.Schema = schema.Schema{MarkdownDescription: d.spec.description, Attributes: attributes}
}

// collectionID binds the listing to its warehouse, database, and filters, so two differently filtered
// listings never share an identity.
func (d *collectionDataSource) collectionID(data types.Object) types.String {
	fields := map[string]string{}
	attributes := data.Attributes()
	for _, name := range slices.Sorted(maps.Keys(d.spec.filters)) {
		value := attributes[name]
		if value == nil || value.IsNull() || value.IsUnknown() {
			continue
		}
		if text, ok := value.(types.String); ok {
			fields[name] = text.ValueString()
		} else {
			fields[name] = value.String()
		}
	}
	database := d.database.ValueString()
	if selected, ok := fields["database"]; ok {
		database = selected
	}
	return d.identity(database, fields)
}

// Read lists the matching objects; no match yields an empty list.
func (d *collectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rows, err := d.spec.list(ctx, &d.resourceClient, data)
	if err != nil {
		resp.Diagnostics.AddError("Read "+d.spec.name, err.Error())
		return
	}
	elementType := d.elementType()
	items := make([]attr.Value, 0, len(rows))
	for _, row := range rows {
		item, diagnostics := types.ObjectValue(elementType.AttrTypes, row)
		resp.Diagnostics.Append(diagnostics...)
		if resp.Diagnostics.HasError() {
			return
		}
		items = append(items, item)
	}
	list, diagnostics := types.ListValue(elementType, items)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	lookupValue(&data, collectionItems, list)
	lookupValue(&data, "id", d.collectionID(data))
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}
