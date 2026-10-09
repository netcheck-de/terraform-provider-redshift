package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// datashareTableResource manages explicit table or view membership in a share.
type datashareTableResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// datashareTableModel is the Terraform state for one shared relation.
type datashareTableModel struct {
	// ID records the producer database/share/schema/table identity.
	ID types.String `tfsdk:"id"`
	// Database owns the producer share.
	Database types.String `tfsdk:"database"`
	// Datashare receives this relation.
	Datashare types.String `tfsdk:"datashare"`
	// Schema contains the source relation.
	Schema types.String `tfsdk:"schema"`
	// Table identifies a producer table or view.
	Table types.String `tfsdk:"table"`
}

// newDatashareTableResource constructs an explicit table/view share membership handler.
func newDatashareTableResource() resource.Resource { return &datashareTableResource{} }

// Metadata identifies the datashare table resource to Terraform.
func (r *datashareTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare_table"
}

// Schema defines one share/schema/table membership.
func (r *datashareTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds one existing table or view to a producer datashare schema.",
		Attributes: map[string]schema.Attribute{
			"id":        idAttribute(),
			"database":  schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Local producer database."},
			"datashare": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Producer datashare name."},
			"schema":    schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Schema already added to the share."},
			"table":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Existing table or view to include."},
		},
	}
}

// read checks explicit table membership under the configured warehouse binding.
func (r *datashareTableResource) read(ctx context.Context, data datashareTableModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readDatashareTableQuery(data))
	return len(rows) > 0, err
}

// Create adds the source table or view to the share and verifies membership.
func (r *datashareTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data datashareTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createDatashareTableStatement(data)
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Add table to datashare", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"datashare": data.Datashare.ValueString(), "schema": data.Schema.ValueString(), "table": data.Table.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify datashare table", err.Error())
	} else if !found {
		resp.Diagnostics.AddError("Verify datashare table", "The table is absent from the datashare after adding it.")
	}
}

// ValidateConfig reports a relation Create would reject, during planning once the configuration is known.
func (r *datashareTableResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data datashareTableModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createDatashareTableStatement(data); err != nil {
		resp.Diagnostics.AddError("Invalid datashare table", err.Error())
	}
}

// Read refreshes table membership and removes absent memberships from state.
func (r *datashareTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data datashareTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare table", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update verifies the immutable table membership remains present.
func (r *datashareTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data datashareTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare table", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update datashare table", "The table disappeared; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete removes only share membership, leaving the source relation intact.
func (r *datashareTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data datashareTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropDatashareTableStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, data)
			if err == nil && found {
				resp.Diagnostics.AddError("Remove datashare table", "The table remains in the datashare after removal.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Remove datashare table", err.Error())
	}
}

// ImportState restores the share/schema/table binding from JSON.
func (r *datashareTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "datashare", "schema", "table")
}
