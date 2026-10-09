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

// groupMembershipResource owns one user-to-group relationship.
type groupMembershipResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// groupMembershipModel is the Terraform state for one explicit group membership.
type groupMembershipModel struct {
	// ID records the administration binding and group/user identity.
	ID types.String `tfsdk:"id"`
	// Group is the SQL group receiving the user.
	Group types.String `tfsdk:"group"`
	// User is the database user whose membership is managed.
	User types.String `tfsdk:"user"`
}

// newGroupMembershipResource constructs a single-user membership handler.
func newGroupMembershipResource() resource.Resource { return &groupMembershipResource{} }

// Metadata identifies the group membership resource to Terraform.
func (r *groupMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_membership"
}

// Schema defines the immutable group/user relationship.
func (r *groupMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Owns one user-to-group membership.", Attributes: map[string]schema.Attribute{
		"id":    idAttribute(),
		"group": schema.StringAttribute{Required: true, MarkdownDescription: "SQL group name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"user":  schema.StringAttribute{Required: true, MarkdownDescription: "SQL user name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
	}}
}

// read checks whether the selected user belongs to the group.
func (r *groupMembershipResource) read(ctx context.Context, data groupMembershipModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readGroupMembershipQuery(data))
	return len(rows) != 0, err
}

// reconcile changes only this membership and verifies the requested presence.
func (r *groupMembershipResource) reconcile(ctx context.Context, data groupMembershipModel, desired bool) error {
	found, err := r.read(ctx, data)
	if err != nil || found == desired {
		return err
	}
	statement := dropGroupMembershipStatement(data)
	if desired {
		statement = createGroupMembershipStatement(data)
	}
	if err := r.exec(ctx, r.database.ValueString(), statement); err != nil {
		return err
	}
	found, err = r.read(ctx, data)
	if err == nil && found != desired {
		return fmt.Errorf("group membership did not converge")
	}
	return err
}

// Create records relationship ownership and adds a missing membership.
func (r *groupMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data groupMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"group": data.Group.ValueString(), "user": data.User.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data, true); err != nil {
		resp.Diagnostics.AddError("Create group membership", err.Error())
	}
}

// Read refreshes membership existence and removes absent relationships from state.
func (r *groupMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data groupMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read group membership", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update restores the selected membership if it has disappeared.
func (r *groupMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data groupMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, true); err != nil {
		resp.Diagnostics.AddError("Update group membership", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete removes this user from the group without affecting other members.
func (r *groupMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data groupMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, false); err != nil {
		resp.Diagnostics.AddError("Delete group membership", err.Error())
	}
}

// ImportState restores a group/user relationship from its JSON identity.
func (r *groupMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "group", "user")
}
