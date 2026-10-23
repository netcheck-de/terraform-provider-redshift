package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// datashareMembershipResource owns every current member of one kind in a shared schema.
type datashareMembershipResource struct{ resourceClient }

// datashareMembershipModel is the exclusive membership tuple and its observed coverage.
type datashareMembershipModel struct {
	// ID is the JSON identity of the tuple.
	ID types.String `tfsdk:"id"`
	// Database is the producer database holding the share.
	Database types.String `tfsdk:"database"`
	// Datashare names the outbound share.
	Datashare types.String `tfsdk:"datashare"`
	// Schema is the shared schema whose members are owned.
	Schema types.String `tfsdk:"schema"`
	// Kind selects TABLES or FUNCTIONS.
	Kind types.String `tfsdk:"kind"`
	// All acknowledges exclusive ownership and reports whether every current member is shared.
	All types.Bool `tfsdk:"all"`
}

var _ = registerResource(newDatashareMembershipResource)

// newDatashareMembershipResource constructs an exclusive datashare membership handler.
func newDatashareMembershipResource() resource.Resource { return &datashareMembershipResource{} }

// Metadata registers exclusive membership separately from per-object ownership.
func (r *datashareMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare_membership"
}

// Schema requires explicit acknowledgement because deletion removes all members of the selected kind.
func (r *datashareMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Exclusively owns all current table/view or SQL-function memberships in one producer share/schema. Requires a SQL superuser for every managed read and mutation, so partial catalog visibility cannot be mistaken for complete ownership. Not a snapshot. Do not combine with explicit members or scoped grants for the same kind, or with include_new = true. Destroy removes every current member of the selected kind, including externally added members.",
		Attributes: map[string]schema.Attribute{
			"id":        idAttribute(),
			"database":  schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local producer database."},
			"datashare": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Producer datashare name."},
			"schema":    schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Schema already included in the share, with include_new = false. Periods in schema or source member names are unsupported because the membership catalog does not quote qualification."},
			"kind":      schema.StringAttribute{Required: true, PlanModifiers: replace, Validators: []validator.String{stringvalidator.OneOf("TABLES", "FUNCTIONS")}, MarkdownDescription: "TABLES includes tables and views; FUNCTIONS includes SQL UDF overloads. Exclusive ownership covers this whole kind in the share/schema."},
			"all":       schema.BoolAttribute{Required: true, Validators: []validator.Bool{boolvalidator.Equals(true)}, MarkdownDescription: "Must be true to acknowledge exclusive ownership. Refresh reports false when any current source object of this kind is not shared; the next apply adds all current objects. An empty schema is fully covered."},
		},
	}
	foldNames(&resp.Schema, "database", "datashare", "schema")
}

// ValidateConfig rejects ambiguous catalog identities before a mutation can be planned.
func (r *datashareMembershipResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data datashareMembershipModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, value := range []types.String{data.Database, data.Datashare, data.Schema} {
		if !value.IsUnknown() && value.ValueString() == "" {
			resp.Diagnostics.AddError("Invalid exclusive datashare membership", "database, datashare, and schema must be nonempty")
			return
		}
	}
	if strings.Contains(knownString(data.Schema), ".") {
		resp.Diagnostics.AddError("Invalid exclusive datashare membership", "exclusive membership cannot distinguish catalog names for a schema containing a period")
	}
	if !data.Kind.IsUnknown() && data.Kind.ValueString() != "TABLES" && data.Kind.ValueString() != "FUNCTIONS" {
		resp.Diagnostics.AddError("Invalid exclusive datashare membership", "kind must be TABLES or FUNCTIONS")
	}
	if !data.All.IsUnknown() && !data.All.ValueBool() {
		resp.Diagnostics.AddError("Invalid exclusive datashare membership", "all must be true to acknowledge exclusive ownership")
	}
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := datashareMembershipKind(data); err != nil {
		resp.Diagnostics.AddError("Invalid exclusive datashare membership", err.Error())
	}
}

// readManaged proves visibility before interpreting a missing parent or an empty candidate set. The administration
// database remains usable when the producer database is gone, so genuine deletion can still remove managed state.
func (r *datashareMembershipResource) readManaged(ctx context.Context, data *datashareMembershipModel) (bool, bool, int, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, false, 0, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readDatashareMembershipSuperuserQuery())
	if err != nil {
		return false, false, 0, err
	}
	if len(rows) != 1 {
		return false, false, 0, fmt.Errorf("exclusive datashare membership requires a SQL superuser; current-user visibility could not be verified")
	}
	superuser, err := strconv.ParseBool(rows[0]["usesuper"])
	if err != nil || !superuser {
		return false, false, 0, fmt.Errorf("exclusive datashare membership requires a SQL superuser to verify complete catalog visibility")
	}
	return r.read(ctx, data)
}

// read is observational: lookups compare only visible objects and do not claim exclusive ownership.
func (r *datashareMembershipResource) read(ctx context.Context, data *datashareMembershipModel) (found, includeNew bool, members int, err error) {
	if _, err = datashareMembershipKind(*data); err != nil {
		return found, includeNew, members, err
	}
	parent := datashareSchemaModel{ID: data.ID, Database: data.Database, Datashare: data.Datashare, Schema: data.Schema}
	found, err = (&datashareSchemaResource{r.resourceClient}).read(ctx, &parent)
	if err != nil || !found {
		return found, includeNew, members, err
	}
	includeNew = parent.IncludeNew.ValueBool()
	candidates, readErr := r.selectRows(ctx, data.Database.ValueString(), readDatashareMembershipCandidatesQuery(*data))
	if readErr != nil {
		err = readErr
		return found, includeNew, members, err
	}
	wanted := map[string]int{}
	for _, row := range candidates {
		name := row["member_name"]
		if name == "" || strings.Contains(name, ".") {
			err = fmt.Errorf("exclusive membership cannot distinguish catalog member name %q", name)
			return found, includeNew, members, err
		}
		wanted[data.Schema.ValueString()+"."+name]++
	}
	rows, readErr := r.selectRows(ctx, data.Database.ValueString(), readDatashareMembershipObjectsQuery(*data))
	if readErr != nil {
		err = readErr
		return found, includeNew, members, err
	}
	actual := map[string]int{}
	for _, row := range rows {
		if _, ok := wanted[row["object_name"]]; ok {
			actual[row["object_name"]]++
			members++
		}
	}
	complete := true
	for name, count := range wanted {
		if actual[name] != count {
			complete = false
		}
	}
	data.All = types.BoolValue(complete)
	return found, includeNew, members, err
}

// apply adds every current member of the selected kind after checking that the schema is shared without include_new.
func (r *datashareMembershipResource) apply(ctx context.Context, data *datashareMembershipModel) error {
	if !data.All.ValueBool() {
		return fmt.Errorf("all must be true to acknowledge exclusive ownership")
	}
	statement, err := createDatashareMembershipStatement(*data)
	if err != nil {
		return err
	}
	found, includeNew, _, err := r.readManaged(ctx, data)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("add the schema to the datashare before managing its members")
	}
	if includeNew {
		return fmt.Errorf("exclusive membership requires include_new = false; disable it on the datashare schema first")
	}
	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		return err
	}
	return nil
}

// verify checks full coverage. While only members are missing, it adds the schema's current objects again for a
// bounded number of rounds, because resources applied in parallel can create objects in the schema between the
// write and the check.
func (r *datashareMembershipResource) verify(ctx context.Context, data *datashareMembershipModel) error {
	for round := 1; ; round++ {
		found, includeNew, _, err := r.readManaged(ctx, data)
		if err != nil {
			return err
		}
		if found && !includeNew && data.All.ValueBool() {
			return nil
		}
		if !found || includeNew || round == snapshotRounds {
			return fmt.Errorf("exclusive membership did not converge to full coverage with include_new = false")
		}
		data.All = types.BoolValue(true)
		if err := r.apply(ctx, data); err != nil {
			return err
		}
	}
}

// Create records ownership before verification so a failed readback cannot orphan a successful mutation.
func (r *datashareMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data datashareMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Add exclusive datashare members", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"datashare": data.Datashare.ValueString(), "schema": data.Schema.ValueString(), "kind": data.Kind.ValueString()})
	data.All = types.BoolValue(true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify exclusive datashare membership", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes coverage, removes the membership when its share or schema is gone, and warns while include_new
// shares future objects implicitly.
func (r *datashareMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data datashareMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, includeNew, _, err := r.readManaged(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read exclusive datashare membership", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	if includeNew {
		resp.Diagnostics.AddWarning("Incompatible future-object inclusion", "Set include_new = false on the datashare schema before changing or destroying its exclusive membership owner.")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update repairs incomplete coverage without replacing the exclusive ownership tuple.
func (r *datashareMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data datashareMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Reconcile exclusive datashare members", err.Error())
		return
	}
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify exclusive datashare membership", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete refuses competing future-object ownership before removing the selected kind.
func (r *datashareMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data datashareMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, includeNew, _, err := r.readManaged(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read exclusive datashare membership", err.Error())
		return
	}
	if !found {
		return
	}
	if includeNew {
		resp.Diagnostics.AddError("Remove exclusive datashare members", "Disable include_new before destroying exclusive membership; no members were removed.")
		return
	}
	statement, err := dropDatashareMembershipStatement(data)
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}
	if err == nil {
		_, _, members, readErr := r.readManaged(ctx, &data)
		err = readErr
		if err == nil && members != 0 {
			err = fmt.Errorf("%d members remain after removing the selected kind", members)
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Remove exclusive datashare members", err.Error())
	}
}

// ImportState adopts the exclusive tuple, not an unrecoverable historical snapshot.
func (r *datashareMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "datashare", "schema", "kind")
}
