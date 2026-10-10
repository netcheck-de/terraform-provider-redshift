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

// catalogSpec describes one lookup derived from its paired resource's schema and identity.
type catalogSpec struct {
	// name is the matching Terraform resource/data-source suffix.
	name string
	// factory constructs the paired resource whose schema the lookup mirrors.
	factory func() resource.Resource
	// computed lists configurable resource attributes that the lookup observes instead of accepting.
	computed []string
	// exists adds an exists output and reports a missing relationship as false instead of an error.
	exists bool
	// lookup reads the selected tuple and fills its computed outputs without executing mutations.
	lookup func(context.Context, *resourceClient, *types.Object) (bool, error)
	// identityFields lists the paired resource's JSON identity keys; permission resources supply their own.
	identityFields []string
	// identityDatabase names the attribute holding the database that owns the identity; empty selects the
	// provider's administration database.
	identityDatabase string
	// adjust rewrites the identity fields where the paired resource omits or derives some of them.
	adjust func(map[string]string)
}

// catalogDataSource shares configuration and read-only state handling for permission and relationship lookups.
type catalogDataSource struct {
	// dataSourceClient provides transport-neutral catalog execution.
	dataSourceClient
	// spec is the lookup's resource pairing, identity contract, and catalog read.
	spec catalogSpec
	// attributes contains identity inputs and computed catalog outputs.
	attributes map[string]schema.Attribute
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

// privilegesLookupDescription replaces the resource's authoritative wording, because a lookup reports what
// the catalog holds rather than what Terraform enforces.
const privilegesLookupDescription = "Current explicit privileges for the selected tuple; inherited privileges are excluded."

// lookupExcluded reports resource inputs that cannot be observed: secrets, the version triggers of write-only
// secrets (named <secret>_wo_version), apply-time triggers, and settings the catalog does not document, such as a
// disabled password.
func lookupExcluded(name string, attribute resourceschema.Attribute) bool {
	return attribute.IsWriteOnly() || name == "password_wo" || strings.HasSuffix(name, "_wo_version") ||
		name == "refresh_revision" || name == "password_disabled"
}

// lookupAttributes converts resource attributes for a lookup; observed attributes, and those listed in
// computed, become read-only outputs, while the others stay inputs with their validators.
func lookupAttributes(source map[string]resourceschema.Attribute, observeAll bool, computed []string) map[string]schema.Attribute {
	attributes := map[string]schema.Attribute{}
	for name, attribute := range source {
		if lookupExcluded(name, attribute) {
			continue
		}
		description := lookupDescription(attribute.GetMarkdownDescription())
		if name == "privileges" {
			description = privilegesLookupDescription
		}
		observed := observeAll || attribute.IsComputed() || slices.Contains(computed, name)
		attributes[name] = lookupAttribute(attribute, observed, description)
	}
	return attributes
}

// lookupAttribute converts one resource attribute of any kind, recursing into nested objects so a nested
// output is computed throughout.
func lookupAttribute(attribute resourceschema.Attribute, observed bool, description string) schema.Attribute {
	required, optional := attribute.IsRequired() && !observed, attribute.IsOptional() && !observed
	switch attribute := attribute.(type) {
	case resourceschema.StringAttribute:
		value := schema.StringAttribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.BoolAttribute:
		value := schema.BoolAttribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.Int64Attribute:
		value := schema.Int64Attribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.Int32Attribute:
		value := schema.Int32Attribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.Float32Attribute:
		value := schema.Float32Attribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.DynamicAttribute:
		value := schema.DynamicAttribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.Float64Attribute:
		value := schema.Float64Attribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.NumberAttribute:
		value := schema.NumberAttribute{Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.ListAttribute:
		value := schema.ListAttribute{ElementType: attribute.ElementType, Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.SetAttribute:
		value := schema.SetAttribute{ElementType: attribute.ElementType, Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.MapAttribute:
		value := schema.MapAttribute{ElementType: attribute.ElementType, Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.ObjectAttribute:
		value := schema.ObjectAttribute{AttributeTypes: attribute.AttributeTypes, Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.ListNestedAttribute:
		value := schema.ListNestedAttribute{NestedObject: lookupNestedObject(attribute.NestedObject, observed), Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.SetNestedAttribute:
		value := schema.SetNestedAttribute{NestedObject: lookupNestedObject(attribute.NestedObject, observed), Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.MapNestedAttribute:
		value := schema.MapNestedAttribute{NestedObject: lookupNestedObject(attribute.NestedObject, observed), Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	case resourceschema.SingleNestedAttribute:
		value := schema.SingleNestedAttribute{Attributes: lookupAttributes(attribute.Attributes, observed, nil), Required: required, Optional: optional, Computed: observed, Sensitive: attribute.Sensitive, CustomType: attribute.CustomType, MarkdownDescription: description}
		if !observed {
			value.Validators = attribute.Validators
		}
		return value
	default:
		// Every framework attribute kind is handled above; a new kind must fail at schema construction
		// rather than silently drop an output.
		panic("lookupAttribute: unsupported resource attribute type")
	}
}

// lookupNestedObject converts the element object of a nested collection.
func lookupNestedObject(object resourceschema.NestedAttributeObject, observed bool) schema.NestedAttributeObject {
	value := schema.NestedAttributeObject{Attributes: lookupAttributes(object.Attributes, observed, nil), CustomType: object.CustomType}
	if !observed {
		value.Validators = object.Validators
	}
	return value
}

// newCatalogDataSource reuses a resource's identity schema while making observed values read-only.
func newCatalogDataSource(spec catalogSpec) datasource.DataSource {
	var source resource.SchemaResponse
	paired := spec.factory()
	paired.Schema(context.Background(), resource.SchemaRequest{}, &source)
	attributes := lookupAttributes(source.Schema.Attributes, false, spec.computed)
	if _, ok := attributes["id"]; ok {
		attributes["id"] = dataSourceIDAttribute()
	}
	if spec.exists {
		attributes["exists"] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this explicit relationship exists. False also covers missing parents."}
	}
	if r, ok := paired.(*privilegeResource); ok && spec.identityFields == nil {
		spec.identityFields = r.fields
	}
	return &catalogDataSource{spec: spec, attributes: attributes}
}

// observedID matches the paired resource's identity keys and ownership database, not its SQL query route.
func (d *catalogDataSource) observedID(data types.Object) types.String {
	fields := map[string]string{}
	for _, field := range d.spec.identityFields {
		if value := identityText(data, field); value != "" {
			fields[field] = value
		}
	}
	if d.spec.adjust != nil {
		d.spec.adjust(fields)
	}
	database := d.database.ValueString()
	if d.spec.identityDatabase != "" {
		database = objectString(data, d.spec.identityDatabase)
	}
	return d.identity(database, fields)
}

// identityText renders one identity field. A Bool flag is recorded as "true" only when set and omitted otherwise,
// matching resources that add an opt-in flag to their identity without changing the format of existing IDs.
func identityText(data types.Object, field string) string {
	if flag, ok := data.Attributes()[field].(types.Bool); ok {
		if flag.ValueBool() {
			return "true"
		}
		return ""
	}
	return objectString(data, field)
}

// Metadata identifies the concrete catalog lookup to Terraform.
func (d *catalogDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.name
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
	found, err := d.spec.lookup(ctx, &d.resourceClient, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read "+d.spec.name, err.Error())
		return
	}
	if d.spec.exists {
		lookupValue(&data, "exists", types.BoolValue(found))
	} else if !found {
		resp.Diagnostics.AddError("Lookup target not found", "The selected object or receiving identity for "+d.spec.name+" does not exist.")
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
	return newCatalogDataSource(catalogSpec{name: r.name, factory: factory, computed: []string{"privileges"}, lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		reader := *r
		reader.resourceClient = *client
		observed := *data
		lookupValue(&observed, "id", types.StringNull())
		_, found, err := reader.readPrivileges(ctx, &observed, false)
		if err == nil && found {
			for _, name := range []string{"privileges", "grant_option_privileges"} {
				// Only types with grant options carry the second set.
				if value, ok := observed.Attributes()[name]; ok {
					lookupValue(data, name, value)
				}
			}
		}
		return found, err
	}})
}
