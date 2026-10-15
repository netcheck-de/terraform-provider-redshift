package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	// Owner is the user that owns the role; unset keeps the catalog owner.
	Owner types.String `tfsdk:"owner"`
	// ExternalID associates the role with a native identity provider; unset keeps the catalog value.
	ExternalID types.String `tfsdk:"external_id"`
	// RoleID is the catalog role ID.
	RoleID types.Int64 `tfsdk:"role_id"`
}

var _ = registerResource(newRoleResource)

// newRoleResource constructs a role lifecycle handler.
func newRoleResource() resource.Resource { return &roleResource{} }

// Metadata identifies the role resource to Terraform.
func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the role name, owner, external ID, and stable import identity.
func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Redshift role, its owner, and its identity-provider external ID. Grants and memberships are separate resources.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Role name, including any identity namespace prefix. Changing it replaces the role.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "User that owns the role (`svv_roles.role_owner`), applied with `ALTER ROLE ... OWNER TO`. Unset keeps the current owner, which is the creating user for a new role. Updated in place.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"external_id": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Identifier of the role in a native third-party identity provider (`EXTERNALID`, reported as `svv_roles.external_id`). Updated in place with `ALTER ROLE ... EXTERNALID TO`. Unset keeps the current value, because `ALTER ROLE` cannot remove it.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role_id": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Catalog role ID (`svv_roles.role_id`).",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
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
	if len(rows) != 1 {
		return false, fmt.Errorf("role %q is ambiguous in the catalog", data.Name.ValueString())
	}
	row := rows[0]
	data.Name = types.StringValue(row["role_name"])
	data.Owner = roleOptional(row["role_owner"])
	data.ExternalID = roleOptional(row["external_id"])
	data.RoleID = types.Int64Null()
	if row["role_id"] != "" {
		id, err := strconv.ParseInt(row["role_id"], 10, 64)
		if err != nil {
			return false, fmt.Errorf("decode role ID: %w", err)
		}
		data.RoleID = types.Int64Value(id)
	}
	return true, nil
}

// roleOptional maps an empty catalog value, which is how both transports return NULL, to a null attribute.
func roleOptional(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// roleKnown replaces an unknown planned value with null, which a saved state can hold.
func roleKnown(value types.String) types.String {
	if value.IsUnknown() {
		return types.StringNull()
	}
	return value
}

// roleConverged reports whether the observed role carries every planned option. Unset options are not compared,
// because they keep whatever the catalog holds.
func roleConverged(planned, observed roleModel) bool {
	for _, pair := range [][2]types.String{{planned.Owner, observed.Owner}, {planned.ExternalID, observed.ExternalID}} {
		if want := knownString(pair[0]); want != "" && want != knownString(pair[1]) {
			return false
		}
	}
	return true
}

// Create creates the role, saves its identity, then transfers ownership and verifies catalog convergence.
func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements := createRoleStatements(data)
	if err := r.exec(ctx, r.database.ValueString(), statements[0]); err != nil {
		resp.Diagnostics.AddError("Create Redshift role", err.Error())
		return
	}
	planned := data
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// The role exists now, so its identity is saved before anything else can fail; unknown values cannot be saved.
	data.Owner, data.ExternalID, data.RoleID = roleKnown(data.Owner), roleKnown(data.ExternalID), types.Int64Null()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.exec(ctx, r.database.ValueString(), statements[1:]...); err != nil {
		resp.Diagnostics.AddError("Change Redshift role owner", err.Error())
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift role", err.Error())
		return
	}
	if !found || !roleConverged(planned, data) {
		resp.Diagnostics.AddError("Verify Redshift role", "The role is absent or its owner or external ID differs after creation.")
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

// Update applies owner and external ID changes and verifies them in the catalog.
func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, r.database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update Redshift role", err.Error())
		return
	}
	if err := r.exec(ctx, r.database.ValueString(), alterRoleStatements(previous, data)...); err != nil {
		resp.Diagnostics.AddError("Update Redshift role", err.Error())
		return
	}
	planned := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift role", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Update Redshift role", "The role disappeared during the update; refresh the plan.")
		return
	}
	if !roleConverged(planned, data) {
		resp.Diagnostics.AddError("Verify Redshift role", "The role's owner or external ID differs after updating.")
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
