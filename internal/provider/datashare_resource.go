package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// datashareResource manages an outbound share in its producer database.
type datashareResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// datashareModel is the Terraform state for a producer share.
type datashareModel struct {
	// ID records warehouse, producer database, and share identity.
	ID types.String `tfsdk:"id"`
	// Database owns the outbound share.
	Database types.String `tfsdk:"database"`
	// Name identifies the SQL datashare.
	Name types.String `tfsdk:"name"`
	// PublicAccessible allows public consumer workgroups when enabled.
	PublicAccessible types.Bool `tfsdk:"publicly_accessible"`
}

var _ = registerResource(newDatashareResource)

// newDatashareResource constructs a producer datashare handler.
func newDatashareResource() resource.Resource { return &datashareResource{} }

// Metadata identifies the datashare resource to Terraform.
func (r *datashareResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare"
}

// Schema defines the producer database, share name, and public-access setting.
func (r *datashareResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one producer datashare in a local database; object membership and consumers are separate grants.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local producer database owning the datashare; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Datashare name, unique within the producer namespace; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"publicly_accessible": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Whether public workgroups may consume the share; defaults to `false`. Updated in place.",
			},
		},
	}
}

// read verifies outbound share ownership and refreshes public accessibility.
func (r *datashareResource) read(ctx context.Context, data *datashareModel) (bool, error) {
	target, err := r.connection(data.Database.ValueString())
	if err != nil {
		return false, err
	}
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readDatashareQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := rows[0]
	if len(rows) != 1 || strings.TrimSpace(row["source_database"]) != target.Database || strings.TrimSpace(row["managed_by"]) != "" {
		return false, fmt.Errorf("datashare %q is bound to another database or managed by another service", data.Name.ValueString())
	}
	public, err := strconv.ParseBool(row["is_publicaccessible"])
	if err != nil {
		return false, fmt.Errorf("decode datashare public accessibility: %w", err)
	}
	data.Name = types.StringValue(strings.TrimSpace(row["share_name"]))
	data.PublicAccessible = types.BoolValue(public)
	return true, nil
}

// Create creates the share in its producer database and verifies its catalog state.
func (r *datashareResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data datashareModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), createDatashareStatement(data)); err != nil {
		resp.Diagnostics.AddError("Create datashare", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	expected := data.PublicAccessible
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify datashare", err.Error())
		return
	}
	if !found || !data.PublicAccessible.Equal(expected) {
		resp.Diagnostics.AddError("Verify datashare", "The datashare is absent or its accessibility differs after creation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the share or removes a missing share from state.
func (r *datashareResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data datashareModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read datashare", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update changes public accessibility without rebinding the producer database.
func (r *datashareResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data datashareModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update datashare", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), alterDatashareStatement(data)); err != nil {
		resp.Diagnostics.AddError("Update datashare", err.Error())
		return
	}
	expected := data.PublicAccessible
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify datashare", err.Error())
		return
	}
	if !found || !data.PublicAccessible.Equal(expected) {
		resp.Diagnostics.AddError("Verify datashare", "The datashare is absent or its accessibility differs after updating.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the share after its independently managed dependencies are removed.
func (r *datashareResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data datashareModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropDatashareStatement(data))
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete datashare", "The datashare remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete datashare", err.Error())
	}
}

// ImportState restores the share's producer database and name binding.
func (r *datashareResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name", "database")
}
