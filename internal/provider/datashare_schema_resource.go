package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// datashareSchemaResource manages schema membership and future-object inclusion in a share.
type datashareSchemaResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// datashareSchemaModel is the Terraform state for one share/schema relationship.
type datashareSchemaModel struct {
	// ID records the producer database, share, and schema binding.
	ID types.String `tfsdk:"id"`
	// Database owns the producer share.
	Database types.String `tfsdk:"database"`
	// Datashare receives the schema membership.
	Datashare types.String `tfsdk:"datashare"`
	// Schema identifies the producer source schema.
	Schema types.String `tfsdk:"schema"`
	// IncludeNew includes objects created after the schema joins the share.
	IncludeNew types.Bool `tfsdk:"include_new"`
}

// newDatashareSchemaResource constructs a share schema membership handler.
func newDatashareSchemaResource() resource.Resource { return &datashareSchemaResource{} }

// Metadata identifies the datashare schema resource to Terraform.
func (r *datashareSchemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare_schema"
}

// Schema defines share/schema identity and future-object inclusion.
func (r *datashareSchemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds one local schema to a producer datashare. Tables are separate share members.",
		Attributes: map[string]schema.Attribute{
			"id":          idAttribute(),
			"database":    schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Local producer database."},
			"datashare":   schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Producer datashare name."},
			"schema":      schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Schema included in the share."},
			"include_new": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Automatically include future objects added to this schema; existing objects still need separate memberships."},
		},
	}
}

// read refreshes schema membership and its include_new catalog setting.
func (r *datashareSchemaResource) read(ctx context.Context, data *datashareSchemaModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.queryDatabase(ctx, data.Database.ValueString(),
		"SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas')",
		map[string]string{"share": data.Datashare.ValueString(), "schema": data.Schema.ValueString()})
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("datashare schema %q has ambiguous catalog rows", data.Schema.ValueString())
	}
	includeNew, err := strconv.ParseBool(rows[0]["include_new"])
	if err != nil {
		return false, fmt.Errorf("decode datashare include_new: %w", err)
	}
	data.IncludeNew = types.BoolValue(includeNew)
	return true, nil
}

// Create adds the schema and configures inclusion of future objects.
func (r *datashareSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data datashareSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.queryDatabase(ctx, data.Database.ValueString(), "ALTER DATASHARE "+sqlclient.Identifier(data.Datashare.ValueString())+" ADD SCHEMA "+sqlclient.Identifier(data.Schema.ValueString()), nil); err != nil {
		resp.Diagnostics.AddError("Add schema to datashare", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"datashare": data.Datashare.ValueString(), "schema": data.Schema.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if data.IncludeNew.ValueBool() {
		if _, err := r.queryDatabase(ctx, data.Database.ValueString(), "ALTER DATASHARE "+sqlclient.Identifier(data.Datashare.ValueString())+" SET INCLUDENEW TRUE FOR SCHEMA "+sqlclient.Identifier(data.Schema.ValueString()), nil); err != nil {
			resp.Diagnostics.AddError("Enable future datashare objects", err.Error())
			return
		}
	}
	expected := data.IncludeNew
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Verify datashare schema", err.Error())
	case !found || !data.IncludeNew.Equal(expected):
		resp.Diagnostics.AddError("Verify datashare schema", "The schema is absent or include_new differs after adding it.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Read refreshes schema membership or removes an absent membership from state.
func (r *datashareSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data datashareSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare schema", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update reconciles include_new while preserving share/schema identity.
func (r *datashareSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data datashareSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update datashare schema", err.Error())
		return
	}
	sql := "ALTER DATASHARE " + sqlclient.Identifier(data.Datashare.ValueString()) + " SET INCLUDENEW " + strconv.FormatBool(data.IncludeNew.ValueBool()) + " FOR SCHEMA " + sqlclient.Identifier(data.Schema.ValueString())
	if _, err := r.queryDatabase(ctx, data.Database.ValueString(), sql, nil); err != nil {
		resp.Diagnostics.AddError("Update datashare schema", err.Error())
		return
	}
	expected := data.IncludeNew
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare schema", err.Error())
	case !found || !data.IncludeNew.Equal(expected):
		resp.Diagnostics.AddError("Update datashare schema", "The schema disappeared or include_new did not converge.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete removes the schema from the share without deleting the source schema.
func (r *datashareSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data datashareSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		_, err = r.queryDatabase(ctx, data.Database.ValueString(), "ALTER DATASHARE "+sqlclient.Identifier(data.Datashare.ValueString())+" REMOVE SCHEMA "+sqlclient.Identifier(data.Schema.ValueString()), nil)
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Remove datashare schema", "The schema remains in the datashare after removal.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Remove datashare schema", err.Error())
	}
}

// ImportState restores the producer database/share/schema identity.
func (r *datashareSchemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "datashare", "schema")
}
