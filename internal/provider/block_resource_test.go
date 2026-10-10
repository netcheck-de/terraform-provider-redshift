package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// blockTestResource is an unregistered resource with a required list block containing a single block and a
// write-only attribute, a set block, and an optional single block, so the shared lookup, parity, replacement, and alter checks are proven on every
// block shape before a registered type depends on them.
type blockTestResource struct{}

// newBlockTestResource constructs the block test resource.
func newBlockTestResource() resource.Resource { return &blockTestResource{} }

// Metadata names the test resource; it is never registered with the provider.
func (r *blockTestResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_test"
}

// Schema follows the provider's block conventions: singular block names, a required block that says so in its
// description, and plain lists for primitive values.
func (r *blockTestResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":       schema.StringAttribute{Computed: true, MarkdownDescription: "JSON identity."},
			"database": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Database containing the table. Changing it replaces the table."},
			"name":     schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Table name. Changing it replaces the table."},
			"owner":    schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Owning user."},
		},
		Blocks: map[string]schema.Block{
			"column": schema.ListNestedBlock{
				MarkdownDescription: "At least one `column` block is required. Columns in order; changing it replaces the table.",
				Validators:          []validator.List{listvalidator.IsRequired(), listvalidator.SizeAtLeast(1)},
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name":       schema.StringAttribute{Required: true, MarkdownDescription: "Column name."},
						"type":       schema.StringAttribute{Required: true, MarkdownDescription: "Column type."},
						"encoding":   schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Compression encoding. Defaults to the server's choice."},
						"default_wo": schema.StringAttribute{Optional: true, WriteOnly: true, Sensitive: true, MarkdownDescription: "Write-only default expression."},
					},
					Blocks: map[string]schema.Block{
						"identity": schema.SingleNestedBlock{
							MarkdownDescription: "Identity generation.",
							Attributes: map[string]schema.Attribute{
								"seed": schema.Int64Attribute{Optional: true, MarkdownDescription: "First value."},
								"step": schema.Int64Attribute{Optional: true, MarkdownDescription: "Increment."},
							},
						},
					},
				},
			},
			"unique": schema.SetNestedBlock{
				MarkdownDescription: "Unique constraints, changed in place.",
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"columns": schema.ListAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Constrained columns."},
				}},
			},
			"distribution": schema.SingleNestedBlock{
				MarkdownDescription: "Distribution style; omitted means AUTO.",
				Attributes: map[string]schema.Attribute{
					"style": schema.StringAttribute{Optional: true, MarkdownDescription: "Style."},
					"key":   schema.StringAttribute{Optional: true, MarkdownDescription: "Distribution key column."},
				},
			},
		},
	}
}

// blockTestPolicy is the block test resource's replacement policy; it is not registered because the resource is not.
var blockTestPolicy = map[string]replaceRule{
	"database": replaceAlways, "name": replaceAlways, "owner": replaceNever,
	"column": replaceAlways, "unique": replaceNever, "distribution": replaceNever,
}

// Create is never called; the resource exists only for schema checks.
func (r *blockTestResource) Create(context.Context, resource.CreateRequest, *resource.CreateResponse) {
}

// Read is never called; see Create.
func (r *blockTestResource) Read(context.Context, resource.ReadRequest, *resource.ReadResponse) {}

// Update is never called; see Create.
func (r *blockTestResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

// Delete is never called; see Create.
func (r *blockTestResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
