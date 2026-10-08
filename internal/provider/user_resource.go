package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// userResource manages SQL user identity, capabilities, and write-only password rotation.
type userResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// userModel carries Terraform user attributes; write-only passwords are never retained in state.
type userModel struct {
	// ID records the administration binding and user name.
	ID types.String `tfsdk:"id"`
	// Name identifies the database user.
	Name types.String `tfsdk:"name"`
	// Password represents the write-only configuration attribute and stays null in state.
	Password types.String `tfsdk:"password_wo"`
	// PasswordVersion explicitly requests password rotation when incremented.
	PasswordVersion types.Int64 `tfsdk:"password_wo_version"`
	// Superuser controls the SQL CREATEUSER capability.
	Superuser types.Bool `tfsdk:"superuser"`
	// CreateDB controls permission to create databases.
	CreateDB types.Bool `tfsdk:"create_database"`
}

// newUserResource constructs a user lifecycle handler with write-only password support.
func newUserResource() resource.Resource { return &userResource{} }

// Metadata identifies the user resource to Terraform.
func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

// Schema defines user identity, capabilities, and write-only password rotation.
func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a database user. Password input is write-only and rotations require an explicit version change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Database user name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password_wo": schema.StringAttribute{
				Optional: true, WriteOnly: true, MarkdownDescription: "Password for creation or rotation; never stored in this provider's plan/state.",
			},
			"password_wo_version": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				MarkdownDescription: "Increment to rotate password_wo. Imported users start at version 0 without changing their password.",
			},
			"superuser": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "CREATEUSER privilege; defaults to false.",
			},
			"create_database": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "CREATEDB privilege; defaults to false.",
			},
		},
	}
}

// read refreshes user capabilities without reading a password.
func (r *userResource) read(ctx context.Context, data *userModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.query(ctx, "SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name", map[string]string{"name": data.Name.ValueString()})
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("user %q is ambiguous in the catalog", data.Name.ValueString())
	}
	superuser, err := strconv.ParseBool(rows[0]["usesuper"])
	if err != nil {
		return false, fmt.Errorf("decode user superuser flag: %w", err)
	}
	createDB, err := strconv.ParseBool(rows[0]["usecreatedb"])
	if err != nil {
		return false, fmt.Errorf("decode user CREATEDB flag: %w", err)
	}
	data.Name = types.StringValue(rows[0]["usename"])
	data.Superuser = types.BoolValue(superuser)
	data.CreateDB = types.BoolValue(createDB)
	return true, nil
}

// password retrieves and validates the creation secret from configuration, not state.
func password(ctx context.Context, config resource.CreateRequest) (string, error) {
	var value types.String
	diagnostics := config.Config.GetAttribute(ctx, path.Root("password_wo"), &value)
	if diagnostics.HasError() || value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return "", fmt.Errorf("password_wo must be set when creating a user")
	}
	return value.ValueString(), nil
}

// Create creates the user and verifies its requested administrative capabilities.
func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	secret, err := password(ctx, req)
	if err != nil {
		resp.Diagnostics.AddError("Create Redshift user", err.Error())
		return
	}
	sql := "CREATE USER " + sqlclient.Identifier(data.Name.ValueString()) + " PASSWORD " + sqlclient.Literal(secret)
	if data.Superuser.ValueBool() {
		sql += " CREATEUSER"
	} else {
		sql += " NOCREATEUSER"
	}
	if data.CreateDB.ValueBool() {
		sql += " CREATEDB"
	} else {
		sql += " NOCREATEDB"
	}
	if _, err := r.query(ctx, sql, nil); err != nil {
		resp.Diagnostics.AddError("Create Redshift user", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	r.verify(ctx, &data, resp)
}

// verify checks user existence and capability convergence after creation.
func (r *userResource) verify(ctx context.Context, data *userModel, resp *resource.CreateResponse) {
	expectedSuperuser, expectedCreateDB := data.Superuser, data.CreateDB
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift user", err.Error())
		return
	}
	if !found || !data.Superuser.Equal(expectedSuperuser) || !data.CreateDB.Equal(expectedCreateDB) {
		resp.Diagnostics.AddError("Verify Redshift user", "The user is absent or its privileges do not match after creation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

// Read refreshes non-secret user attributes or removes a missing user from state.
func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift user", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update rotates a versioned password and reconciles user capabilities.
func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, r.database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update Redshift user", err.Error())
		return
	}
	if !previous.PasswordVersion.IsNull() && !data.PasswordVersion.Equal(previous.PasswordVersion) {
		var secret types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("password_wo"), &secret)...)
		if secret.IsNull() || secret.IsUnknown() || secret.ValueString() == "" {
			resp.Diagnostics.AddError("Rotate Redshift password", "password_wo is required when password_wo_version changes.")
			return
		}
		if _, err := r.query(ctx, "ALTER USER "+sqlclient.Identifier(data.Name.ValueString())+" PASSWORD "+sqlclient.Literal(secret.ValueString()), nil); err != nil {
			resp.Diagnostics.AddError("Rotate Redshift password", err.Error())
			return
		}
	}
	for _, change := range []struct {
		before, after types.Bool
		option        string
	}{
		{previous.Superuser, data.Superuser, "CREATEUSER"},
		{previous.CreateDB, data.CreateDB, "CREATEDB"},
	} {
		if change.before.IsNull() || change.before.Equal(change.after) {
			continue
		}
		option := change.option
		if !change.after.ValueBool() {
			option = "NO" + option
		}
		if _, err := r.query(ctx, "ALTER USER "+sqlclient.Identifier(data.Name.ValueString())+" "+option, nil); err != nil {
			resp.Diagnostics.AddError("Update Redshift user", err.Error())
			return
		}
	}
	expectedSuperuser, expectedCreateDB := data.Superuser, data.CreateDB
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift user", err.Error())
		return
	}
	if !found || !data.Superuser.Equal(expectedSuperuser) || !data.CreateDB.Equal(expectedCreateDB) {
		resp.Diagnostics.AddError("Verify Redshift user", "The user is absent or its privileges do not match after updating.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the user after checking its binding and verifies removal.
func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		_, err = r.query(ctx, "DROP USER "+sqlclient.Identifier(data.Name.ValueString()), nil)
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete Redshift user", "The user remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete Redshift user", err.Error())
	}
}

// ImportState restores user identity without importing its password.
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("password_wo_version"), int64(0))...)
}
