package provider

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// catalogDataSource shares configuration and read-only state handling for permission and relationship lookups.
type catalogDataSource struct {
	// dataSourceClient provides transport-neutral catalog execution.
	dataSourceClient
	// name is the matching Terraform resource/data-source suffix.
	name string
	// attributes contains identity inputs and computed catalog outputs.
	attributes map[string]schema.Attribute
	// identityFields contains the paired resource's JSON identity keys.
	identityFields []string
	// lookup reads the selected tuple and fills its computed outputs without executing mutations.
	lookup func(context.Context, *resourceClient, *types.Object) (bool, error)
}

// lookupValue replaces one computed output while preserving the data source's object type.
func lookupValue(data *types.Object, name string, value attr.Value) {
	attributes := data.Attributes()
	attributes[name] = value
	*data = types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes)
}

// replacementNote matches resource-only replacement wording that does not apply to lookups.
var replacementNote = regexp.MustCompile(`(;\s*c|\s*C)hanging it replaces the \w+\.`)

// lookupDescription removes replacement notes from a paired resource's attribute description.
func lookupDescription(description string) string {
	return replacementNote.ReplaceAllStringFunc(description, func(note string) string {
		if strings.HasPrefix(note, ";") {
			return "."
		}
		return ""
	})
}

// newCatalogDataSource reuses a resource's identity schema while making observed values read-only.
func newCatalogDataSource(name string, factory func() resource.Resource, computed []string, exists bool, lookup func(context.Context, *resourceClient, *types.Object) (bool, error)) datasource.DataSource {
	var source resource.SchemaResponse
	paired := factory()
	paired.Schema(context.Background(), resource.SchemaRequest{}, &source)
	attributes := map[string]schema.Attribute{}
	for name, attribute := range source.Schema.Attributes {
		if name == "password_wo" || name == "password_wo_version" || name == "refresh_revision" {
			continue
		}
		if name == "id" {
			attributes[name] = dataSourceIDAttribute()
			continue
		}
		observed := attribute.IsComputed() || slices.Contains(computed, name)
		switch attribute := attribute.(type) {
		case resourceschema.StringAttribute:
			value := schema.StringAttribute{Required: attribute.Required && !observed, Optional: attribute.Optional && !observed, Computed: observed, MarkdownDescription: lookupDescription(attribute.MarkdownDescription)}
			if !observed {
				value.Validators = attribute.Validators
			}
			attributes[name] = value
		case resourceschema.BoolAttribute:
			attributes[name] = schema.BoolAttribute{Computed: true, MarkdownDescription: lookupDescription(attribute.MarkdownDescription)}
		case resourceschema.SetAttribute:
			attributes[name] = schema.SetAttribute{Computed: true, ElementType: attribute.ElementType, MarkdownDescription: "Current explicit privileges for the selected tuple; inherited privileges are excluded."}
		}
	}
	if exists {
		attributes["exists"] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this explicit relationship exists. False also covers missing parents."}
	}
	fields := map[string][]string{
		"datashare_grant":  {"datashare", "account_id", "namespace_id"},
		"datashare_schema": {"datashare", "schema"},
		"datashare_table":  {"datashare", "schema", "table"},
		"group_membership": {"group", "user"},
		"role_grant":       {"role", "to_user", "to_role"},
		"grant":            {"database_name", "role", "datashare", "scope", "schema_name"},
		"comment":          {"database_name", "object_type", "object_name", "schema_name", "column_name"},
	}[name]
	if r, ok := paired.(*privilegeResource); ok {
		fields = r.fields
	}
	return &catalogDataSource{name: name, attributes: attributes, identityFields: fields, lookup: lookup}
}

// observedID matches the paired resource's identity keys and ownership database, not its SQL query route.
func (d *catalogDataSource) observedID(data types.Object) types.String {
	fields := map[string]string{}
	for _, field := range d.identityFields {
		if value := objectString(data, field); value != "" {
			fields[field] = value
		}
	}
	if d.name == "grant" && fields["datashare"] != "" {
		delete(fields, "role")
	}
	database := d.database.ValueString()
	if d.name == "datashare_grant" || d.name == "datashare_schema" || d.name == "datashare_table" {
		database = objectString(data, "database")
	}
	return d.identity(database, fields)
}

// Metadata identifies the concrete catalog lookup to Terraform.
func (d *catalogDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.name
}

// Schema exposes immutable lookup inputs and computed catalog observations and identity.
func (d *catalogDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the selected SQL relationship, explicit permissions, or annotation without taking ownership or executing mutations.", Attributes: d.attributes}
}

// Read reports missing relationships as exists=false and missing permission/annotation parents as errors.
func (d *catalogDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := d.lookup(ctx, &d.resourceClient, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read "+d.name, err.Error())
		return
	}
	if _, relationship := d.attributes["exists"]; relationship {
		lookupValue(&data, "exists", types.BoolValue(found))
	} else if !found {
		resp.Diagnostics.AddError("Lookup target not found", "The selected object or receiving identity for "+d.name+" does not exist.")
		return
	}
	id := types.StringNull()
	if found {
		id = d.observedID(data)
	}
	lookupValue(&data, "id", id)
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

// newPrivilegeDataSource observes a permission resource's catalog contract without its mutation restrictions.
func newPrivilegeDataSource(factory func() resource.Resource) datasource.DataSource {
	r := factory().(*privilegeResource)
	return newCatalogDataSource(r.name, factory, []string{"privileges"}, false, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		reader := *r
		reader.resourceClient = *client
		observed := *data
		lookupValue(&observed, "id", types.StringNull())
		_, found, err := reader.readPrivileges(ctx, &observed, false)
		if err == nil && found {
			lookupValue(data, "privileges", observed.Attributes()["privileges"])
		}
		return found, err
	})
}
