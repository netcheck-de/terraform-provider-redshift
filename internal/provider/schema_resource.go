package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// schemaResource manages the existence of a local SQL schema.
type schemaResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// schemaModel is the Terraform state for a local schema and its observed owner.
type schemaModel struct {
	// ID records the warehouse/database/schema identity.
	ID types.String `tfsdk:"id"`
	// Database contains the schema.
	Database types.String `tfsdk:"database"`
	// Name identifies the SQL schema.
	Name types.String `tfsdk:"name"`
	// Owner is the catalog-reported SQL owner, not a managed ownership grant.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerResource(newSchemaResource)

// newSchemaResource constructs a local schema lifecycle handler.
func newSchemaResource() resource.Resource { return &schemaResource{} }

// Metadata identifies the schema resource to Terraform.
func (r *schemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

// Schema defines local schema identity and its observed owner.
func (r *schemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one local schema in a Redshift database; external schemas are not supported.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local database owning the schema; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Schema name; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"owner": schema.StringAttribute{Computed: true, MarkdownDescription: "Database user owning the schema."},
		},
	}
}

// read refreshes the schema and its owner under the configured database binding.
func (r *schemaResource) read(ctx context.Context, data *schemaModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readSchemaQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 || rows[0]["owner"] == "" {
		return false, fmt.Errorf("schema %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
	}
	data.Name, data.Owner = types.StringValue(rows[0]["schema_name"]), types.StringValue(rows[0]["owner"])
	return true, nil
}

// Create creates an empty local schema and reads its assigned owner.
func (r *schemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), createSchemaStatement(data)); err != nil {
		resp.Diagnostics.AddError("Create schema", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// A failed verification must still return serializable state for the created schema.
	data.Owner = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Verify schema", err.Error())
	case !found:
		resp.Diagnostics.AddError("Verify schema", "The schema is absent after creation.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Read refreshes schema attributes or removes a missing schema from state.
func (r *schemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read schema", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update verifies the immutable schema identity and refreshes its owner.
func (r *schemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read schema", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update schema", "The schema disappeared during the update; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete drops the schema restrictively and verifies catalog removal.
func (r *schemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropSchemaStatement(data))
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete schema", "The schema remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete schema", err.Error())
	}
}

// ImportState restores schema database/name ownership from JSON.
func (r *schemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
