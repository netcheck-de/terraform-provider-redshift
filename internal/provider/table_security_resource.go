package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// tableSecurityResource owns the row-level security switch of one relation; policies and their attachments are
// separate resources, so attaching a policy never enables RLS as a side effect.
type tableSecurityResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// tableSecurityModel is the Terraform state of one relation's row-level security settings.
type tableSecurityModel struct {
	// ID records the warehouse/database/schema/relation identity.
	ID types.String `tfsdk:"id"`
	// Database contains the relation.
	Database types.String `tfsdk:"database"`
	// Schema contains the relation.
	Schema types.String `tfsdk:"schema"`
	// Relation is the protected table or view.
	Relation types.String `tfsdk:"relation"`
	// RowLevelSecurity is the relation's RLS switch.
	RowLevelSecurity types.Bool `tfsdk:"row_level_security"`
	// ConjunctionType combines several attached policies with AND or OR.
	ConjunctionType types.String `tfsdk:"conjunction_type"`
	// DatashareRowLevelSecurity is the FOR DATASHARES switch.
	DatashareRowLevelSecurity types.Bool `tfsdk:"datashare_row_level_security"`
}

var _ = registerResource(newTableSecurityResource)

// newTableSecurityResource constructs a relation row-level security handler.
func newTableSecurityResource() resource.Resource { return &tableSecurityResource{} }

// Metadata identifies the table security resource to Terraform.
func (r *tableSecurityResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table_security"
}

// Schema defines the relation identity and its in-place row-level security settings.
func (r *tableSecurityResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Turns row-level security (RLS) on or off for one existing table or view with `ALTER TABLE ... ROW LEVEL SECURITY`. **Destroying this resource turns row-level security OFF for the relation**, so every attached policy stops filtering and all rows become visible to anyone with `SELECT`.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local database containing the relation. Changing it replaces the table security."},
			"schema":   schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Schema of the relation. Changing it replaces the table security."},
			"relation": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Table, view, late-binding view, or materialized view; the resource never creates or drops it. Changing it replaces the table security."},
			"row_level_security": schema.BoolAttribute{
				Required: true, MarkdownDescription: "Whether row-level security is on. While it is on, users see only the rows an attached policy permits, and no rows when no policy applies to them; superusers and `sys:secadmin` holders are exempt.",
			},
			"conjunction_type": schema.StringAttribute{
				Optional: true, Computed: true, MarkdownDescription: "`AND` or `OR`: how several policies attached for the same user combine (`CONJUNCTION TYPE`). Redshift defaults to `AND`; unset keeps the current value.",
				Validators:    []validator.String{stringvalidator.OneOf("AND", "OR")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"datashare_row_level_security": schema.BoolAttribute{
				Optional: true, Computed: true, MarkdownDescription: "Row-level security for datashare consumers (`ROW LEVEL SECURITY { ON | OFF } FOR DATASHARES`). While it is on, which is the default, consumer queries of the RLS-protected relation fail; `false` makes the relation readable through datashares **without any row filtering on the consumer side**. Unset keeps the current value.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// tableSecurityIdentity is the JSON identity shared by the resource, its lookup, and imports.
func (r *tableSecurityResource) tableSecurityIdentity(data tableSecurityModel) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "relation": data.Relation.ValueString()})
}

// read refreshes the settings. A relation that svv_rls_relation does not list has never been protected, so it
// reports the documented defaults: RLS off, AND, and RLS on for datashares.
func (r *tableSecurityResource) read(ctx context.Context, data *tableSecurityModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	relations, err := r.selectRows(ctx, data.Database.ValueString(), readTableSecurityRelationQuery(*data))
	if err != nil || len(relations) == 0 {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readTableSecurityQuery(*data))
	if err != nil {
		return false, err
	}
	on, datashare, conjunction := false, true, "AND"
	switch len(rows) {
	case 0:
	case 1:
		if on, err = strconv.ParseBool(rows[0]["is_rls_on"]); err != nil {
			return false, fmt.Errorf("parse is_rls_on: %w", err)
		}
		if datashare, err = strconv.ParseBool(rows[0]["is_rls_datashare_on"]); err != nil {
			return false, fmt.Errorf("parse is_rls_datashare_on: %w", err)
		}
		// The column is CHAR(3), so OR arrives padded.
		if reported := strings.ToUpper(strings.TrimSpace(rows[0]["rls_conjunction_type"])); reported != "" {
			conjunction = reported
		}
	default:
		return false, fmt.Errorf("relation %s.%s has ambiguous row-level security metadata", data.Schema.ValueString(), data.Relation.ValueString())
	}
	data.RowLevelSecurity, data.DatashareRowLevelSecurity, data.ConjunctionType = types.BoolValue(on), types.BoolValue(datashare), types.StringValue(conjunction)
	return true, nil
}

// verify re-reads the settings after Create or Update and fails unless every planned value took effect.
func (r *tableSecurityResource) verify(ctx context.Context, data *tableSecurityModel, planned tableSecurityModel) error {
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return fmt.Errorf("relation %s.%s does not exist", data.Schema.ValueString(), data.Relation.ValueString())
	case !data.RowLevelSecurity.Equal(planned.RowLevelSecurity):
		return fmt.Errorf("the catalog reports row_level_security = %s instead of %s", data.RowLevelSecurity, planned.RowLevelSecurity)
	case knownString(planned.ConjunctionType) != "" && data.ConjunctionType.ValueString() != planned.ConjunctionType.ValueString():
		return fmt.Errorf("the catalog reports conjunction_type = %s instead of %s", data.ConjunctionType, planned.ConjunctionType)
	case knownBool(planned.DatashareRowLevelSecurity) != nil && !data.DatashareRowLevelSecurity.Equal(planned.DatashareRowLevelSecurity):
		return fmt.Errorf("the catalog reports datashare_row_level_security = %s instead of %s", data.DatashareRowLevelSecurity, planned.DatashareRowLevelSecurity)
	}
	return nil
}

// Create validates the settings before the first state write, applies them, and verifies the catalog.
func (r *tableSecurityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data tableSecurityModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := createTableSecurityStatements(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid table security", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Set table row-level security", err.Error())
		return
	}
	data.ID = r.tableSecurityIdentity(data)
	planned := data
	// A failed verification must still leave serializable state for the changed relation.
	if data.ConjunctionType.IsUnknown() {
		data.ConjunctionType = types.StringNull()
	}
	if data.DatashareRowLevelSecurity.IsUnknown() {
		data.DatashareRowLevelSecurity = types.BoolNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.verify(ctx, &data, planned); err != nil {
		resp.Diagnostics.AddError("Verify table row-level security", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ValidateConfig reports settings Create would reject, during planning once the configuration is known.
func (r *tableSecurityResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data tableSecurityModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createTableSecurityStatements(data); err != nil {
		resp.Diagnostics.AddError("Invalid table security", err.Error())
	}
}

// Read refreshes the settings and removes state when the relation or its database is gone.
func (r *tableSecurityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data tableSecurityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read table row-level security", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update applies the changed settings in place and verifies them.
func (r *tableSecurityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior tableSecurityModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(prior.ID, prior.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update table row-level security", err.Error())
		return
	}
	statements, err := alterTableSecurityStatements(prior, plan)
	if err == nil {
		err = r.exec(ctx, plan.Database.ValueString(), statements...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update table row-level security", err.Error())
		return
	}
	planned := plan
	if err := r.verify(ctx, &plan, planned); err != nil {
		resp.Diagnostics.AddError("Verify table row-level security", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete turns row-level security off and verifies it; the relation, its policies, and their attachments stay.
func (r *tableSecurityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data tableSecurityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found && data.RowLevelSecurity.ValueBool() {
		var statement string
		if statement, err = deleteTableSecurityStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found && data.RowLevelSecurity.ValueBool() {
				resp.Diagnostics.AddError("Turn off table row-level security", "Row-level security is still on after ROW LEVEL SECURITY OFF.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Turn off table row-level security", err.Error())
	}
}

// ImportState restores the database/schema/relation binding from JSON; refresh reads the settings.
func (r *tableSecurityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "relation")
}
