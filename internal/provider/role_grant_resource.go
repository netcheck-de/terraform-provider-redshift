package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// roleGrantResource manages one role grant to a user or another role.
type roleGrantResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// roleGrantModel is the Terraform state for one granted role and one recipient.
type roleGrantModel struct {
	// ID records the administration binding and role/recipient tuple.
	ID types.String `tfsdk:"id"`
	// Role is the granted SQL role, including built-in system roles.
	Role types.String `tfsdk:"role"`
	// ToRole selects a role recipient and conflicts with ToUser.
	ToRole types.String `tfsdk:"to_role"`
	// ToUser selects a user recipient and conflicts with ToRole.
	ToUser types.String `tfsdk:"to_user"`
}

// newRoleGrantResource constructs a role-to-role or role-to-user grant handler.
func newRoleGrantResource() resource.Resource { return &roleGrantResource{} }

// Metadata identifies the role grant resource to Terraform.
func (r *roleGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_grant"
}

// Schema defines one granted role and exactly one recipient type.
func (r *roleGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants one role to a receiving role or database user, including sys: roles.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"role": schema.StringAttribute{
				Required: true, MarkdownDescription: "Role being granted, including built-in `sys:` roles.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"to_role": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Receiving role; exactly one of to_role and to_user is required.",
				Validators:    []validator.String{stringvalidator.ExactlyOneOf(path.MatchRoot("to_role"), path.MatchRoot("to_user"))},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"to_user": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Receiving database user; exactly one of to_role and to_user is required.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

// read validates recipient selection and checks explicit role membership.
func (r *roleGrantResource) read(ctx context.Context, data roleGrantModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readRoleGrantQuery(data))
	return len(rows) > 0, err
}

// fields returns only the selected recipient's import identity fields.
func (data roleGrantModel) fields() map[string]string {
	fields := map[string]string{"role": data.Role.ValueString()}
	if !data.ToUser.IsNull() {
		fields["to_user"] = data.ToUser.ValueString()
	} else {
		fields["to_role"] = data.ToRole.ValueString()
	}
	return fields
}

// Create grants the role idempotently and verifies explicit membership.
func (r *roleGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data roleGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Save the stable identity before post-write verification can fail.
	if err := r.exec(ctx, r.database.ValueString(), createRoleGrantStatement(data)); err != nil {
		resp.Diagnostics.AddError("Grant Redshift role membership", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), data.fields())
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift role membership", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Verify Redshift role membership", "The membership is absent after granting.")
	}
}

// Read refreshes role membership and removes missing relationships from state.
func (r *roleGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data roleGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift role membership", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update verifies the immutable role relationship remains present.
func (r *roleGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data roleGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Update Redshift role membership", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update Redshift role membership", "The membership disappeared during the update; refresh the plan.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete revokes only the selected role membership and verifies removal.
func (r *roleGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data roleGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err == nil && found {
		err = r.exec(ctx, r.database.ValueString(), dropRoleGrantStatement(data))
		if err == nil {
			found, err = r.read(ctx, data)
			if err == nil && found {
				resp.Diagnostics.AddError("Revoke Redshift role membership", "The membership remains after revoking.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Revoke Redshift role membership", err.Error())
	}
}

// ImportState validates and restores exactly one role grant recipient.
func (r *roleGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	toRole, toUser := values["to_role"], values["to_user"]
	if (toRole == "") == (toUser == "") {
		resp.Diagnostics.AddError("Invalid import identity", "Specify exactly one of to_role and to_user.")
		return
	}
	field := "to_role"
	if toUser != "" {
		field = "to_user"
	}
	importIdentity(ctx, req, resp, "role", field)
}
