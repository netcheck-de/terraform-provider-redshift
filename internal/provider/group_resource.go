package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// groupResource manages a SQL user group independently of its members and privileges.
type groupResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// groupModel is the Terraform state for one SQL user group.
type groupModel struct {
	// ID records the administration binding and group name.
	ID types.String `tfsdk:"id"`
	// Name identifies the SQL group, not an Identity Center role.
	Name types.String `tfsdk:"name"`
}

// newGroupResource constructs a SQL group lifecycle handler.
func newGroupResource() resource.Resource { return &groupResource{} }

// Metadata identifies the group resource to Terraform.
func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines group identity while leaving membership ownership separate.
func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages a SQL user group. Memberships and grants are separate resources.", Attributes: map[string]schema.Attribute{
		"id":   idAttribute(),
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "SQL group name; changing it replaces the resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
	}}
}

// read verifies the binding and checks group existence in pg_group.
func (r *groupResource) read(ctx context.Context, data *groupModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.query(ctx, "SELECT groname FROM pg_group WHERE groname = :name", map[string]string{"name": data.Name.ValueString()})
	return len(rows) != 0, err
}

// Create creates an empty group and verifies it appears in the catalog.
func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data groupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.query(ctx, "CREATE GROUP "+sqlclient.Identifier(data.Name.ValueString()), nil); err != nil {
		resp.Diagnostics.AddError("Create Redshift group", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift group", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Verify Redshift group", "The group is absent after creation.")
	}
}

// Read refreshes group existence, removing missing groups from state.
func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data groupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift group", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update verifies the immutable group remains present.
func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data groupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Update Redshift group", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update Redshift group", "The group disappeared; refresh the plan.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the group without deleting users or revoking unrelated privileges.
func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data groupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		_, err = r.query(ctx, "DROP GROUP "+sqlclient.Identifier(data.Name.ValueString()), nil)
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete Redshift group", "The group remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete Redshift group", err.Error())
	}
}

// ImportState restores a group's warehouse, database, and name binding.
func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
}
