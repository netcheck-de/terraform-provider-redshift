package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// roleResource manages SQL role identity independently of memberships and privileges.
type roleResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// roleModel is the Terraform state for one SQL role.
type roleModel struct {
	// ID records the administration binding and role name.
	ID types.String `tfsdk:"id"`
	// Name includes an identity-provider namespace prefix when applicable.
	Name types.String `tfsdk:"name"`
}

// newRoleResource constructs a role lifecycle handler.
func newRoleResource() resource.Resource { return &roleResource{} }

// Metadata identifies the role resource to Terraform.
func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the role name and stable import identity.
func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Redshift role. Grants and memberships are separate resources.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Role name, including any identity namespace prefix; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

// read verifies warehouse ownership and refreshes an existing role.
func (r *roleResource) read(ctx context.Context, data *roleModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readRoleQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	data.Name = types.StringValue(rows[0]["role_name"])
	return true, nil
}

// Create creates the role and retains its identity before verifying catalog convergence.
func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.exec(ctx, r.database.ValueString(), createRoleStatement(data)); err != nil {
		resp.Diagnostics.AddError("Create Redshift role", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift role", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Verify Redshift role", "The role is absent after creation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the role or removes a missing role from state.
func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift role", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update verifies the immutable role still exists under the configured binding.
func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Only the administration connection can change in-place.
	var data roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift role", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update Redshift role", "The role disappeared during the update; refresh the plan.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the role without cascading and verifies its removal.
func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, r.database.ValueString(), dropRoleStatement(data))
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete Redshift role", "The role remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete Redshift role", err.Error())
	}
}

// ImportState restores a role's JSON identity without executing creation SQL.
func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
}
