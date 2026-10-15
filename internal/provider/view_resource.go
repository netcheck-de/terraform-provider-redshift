package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// viewResource manages an ordinary or late-binding view.
type viewResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// viewModel is the Terraform state of one view.
type viewModel struct {
	// ID records the warehouse/database/schema/name identity.
	ID types.String `tfsdk:"id"`
	// Database contains the view.
	Database types.String `tfsdk:"database"`
	// Schema contains the view.
	Schema types.String `tfsdk:"schema"`
	// Name identifies the view within its schema.
	Name types.String `tfsdk:"name"`
	// Query is the configured SELECT, or the catalog definition after import or an outside change.
	Query types.String `tfsdk:"query"`
	// LateBinding selects WITH NO SCHEMA BINDING.
	LateBinding types.Bool `tfsdk:"late_binding"`
	// Owner is the view's SQL owner.
	Owner types.String `tfsdk:"owner"`
	// DefinitionFingerprint detects definition changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
}

var _ = registerResource(newViewResource)

// newViewResource constructs a view lifecycle handler.
func newViewResource() resource.Resource { return &viewResource{} }

// Metadata identifies the view resource to Terraform.
func (r *viewResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_view"
}

// viewNameAttribute is a required identifier that replaces the view when it changes.
func viewNameAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Required: true, MarkdownDescription: description,
		Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

// viewOwnerAttribute is the optional owner shared by views and materialized views. Without configuration it
// reports the catalog owner, and UseStateForUnknown keeps unrelated updates from planning an owner change.
func viewOwnerAttribute(noun string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true, Computed: true,
		MarkdownDescription: "SQL user owning the " + noun + ". When set, it is applied with `ALTER TABLE ... OWNER TO`; when unset, the creating user owns the " + noun + " and the attribute reports the catalog owner.",
		Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

// Schema defines the view identity, its definition, and its owner.
func (r *viewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an ordinary or late-binding view in a local Redshift schema.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": viewNameAttribute("Local database containing the view. Changing it replaces the view."),
			"schema":   viewNameAttribute("Existing schema containing the view. Changing it replaces the view."),
			"name":     viewNameAttribute("View name. Changing it replaces the view."),
			"query": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "SELECT statement defining the view, without a trailing `;`. A change runs `CREATE OR REPLACE VIEW`, " +
					"which Redshift accepts only when the new query returns the same column names and types; otherwise the apply fails " +
					"and the view is left unchanged. State keeps this text while the catalog definition is unchanged; after an import " +
					"or a change made outside Terraform it holds the catalog definition, so the next plan restores the configured query.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"late_binding": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Create a late-binding view with `WITH NO SCHEMA BINDING`, so referenced tables need not exist and may be " +
					"dropped or altered independently. Tables in the query must then be schema-qualified. A change runs `CREATE OR REPLACE VIEW`. Defaults to `false`.",
			},
			"owner":                  viewOwnerAttribute("view"),
			"definition_fingerprint": definitionFingerprintAttribute(),
		},
	}
}

// viewCatalogEntry is one pg_views row, classified by its definition text.
type viewCatalogEntry struct {
	// owner is the catalog owner name.
	owner string
	// definition is the pg_get_viewdef text.
	definition string
	// lateBinding reports WITH NO SCHEMA BINDING.
	lateBinding bool
	// materialized reports a materialized view.
	materialized bool
}

// readViewCatalog reads one relation from pg_views. A missing database or relation is reported as not found,
// so refresh removes the state of a view whose parent is gone.
func readViewCatalog(ctx context.Context, client *resourceClient, database, schemaName, name string) (viewCatalogEntry, bool, error) {
	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return viewCatalogEntry{}, false, err
	}
	rows, err := client.selectRows(ctx, database, viewCatalogQuery(schemaName, name))
	if err != nil || len(rows) == 0 {
		return viewCatalogEntry{}, false, err
	}
	if len(rows) != 1 || rows[0]["viewowner"] == "" || rows[0]["definition"] == "" {
		return viewCatalogEntry{}, false, fmt.Errorf("view %s.%s has incomplete or ambiguous catalog metadata", schemaName, name)
	}
	entry := viewCatalogEntry{owner: rows[0]["viewowner"], definition: rows[0]["definition"]}
	entry.lateBinding, entry.materialized = viewDefinitionKind(entry.definition)
	return entry, true, nil
}

// errViewIsMaterialized keeps the view resource from adopting a materialized view, whose lifecycle differs.
var errViewIsMaterialized = errors.New("the relation is a materialized view; manage it with redshift_materialized_view")

// read checks the binding and reads the view.
func (r *viewResource) read(ctx context.Context, data viewModel) (viewCatalogEntry, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return viewCatalogEntry{}, false, err
	}
	entry, found, err := readViewCatalog(ctx, &r.resourceClient, data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString())
	if err == nil && found && entry.materialized {
		err = errViewIsMaterialized
	}
	return entry, found, err
}

// converge re-reads the view after apply, checks that the binding mode and a configured owner took effect, and
// records the configured query with the fingerprint of the catalog definition.
func (r *viewResource) converge(ctx context.Context, data *viewModel, planned viewModel) error {
	entry, found, err := r.read(ctx, *data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the view is absent after apply")
	case entry.lateBinding != planned.LateBinding.ValueBool():
		return fmt.Errorf("the view's late binding is %t after apply, expected %t", entry.lateBinding, planned.LateBinding.ValueBool())
	case knownString(planned.Owner) != "" && entry.owner != planned.Owner.ValueString():
		return fmt.Errorf("the view is owned by %q after apply, expected %q", entry.owner, planned.Owner.ValueString())
	}
	data.Owner, data.LateBinding = types.StringValue(entry.owner), types.BoolValue(entry.lateBinding)
	data.Query, data.DefinitionFingerprint = recordDefinition(planned.Query, entry.definition)
	return nil
}

// Create validates the definition, creates the view, applies a configured owner, and verifies the result.
func (r *viewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data viewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := createViewStatements(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid view", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements[0]); err != nil {
		resp.Diagnostics.AddError("Create view", err.Error())
		return
	}
	planned := data
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString()})
	// A failed ownership change or verification must still record the created view in serializable state.
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	data.DefinitionFingerprint = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.exec(ctx, data.Database.ValueString(), statements[1:]...); err != nil {
		resp.Diagnostics.AddError("Set view owner", err.Error())
		return
	}
	if err := r.converge(ctx, &data, planned); err != nil {
		resp.Diagnostics.AddError("Verify view", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ValidateConfig reports a definition Create would reject, during planning once the configuration is known.
func (r *viewResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data viewModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createViewStatements(data); err != nil {
		resp.Diagnostics.AddError("Invalid view", err.Error())
	}
}

// Read refreshes the owner and binding mode, surfaces an outside definition change, or removes a missing view.
func (r *viewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data viewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	entry, found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read view", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	data.Owner, data.LateBinding = types.StringValue(entry.owner), types.BoolValue(entry.lateBinding)
	data.Query, data.DefinitionFingerprint = reconcileDefinition(data.Query, data.DefinitionFingerprint, entry.definition)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update replaces the definition and transfers ownership in place, then verifies the result.
func (r *viewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous viewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := alterViewStatements(previous, data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid view", err.Error())
		return
	}
	// CREATE OR REPLACE would silently recreate a view dropped since the plan, so the view must still exist.
	_, found, err := r.read(ctx, previous)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read view", err.Error())
		return
	case !found:
		resp.Diagnostics.AddError("Update view", "The view disappeared during the update; refresh the plan.")
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update view", err.Error())
		return
	}
	if err := r.converge(ctx, &data, data); err != nil {
		resp.Diagnostics.AddError("Verify view", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the view restrictively and verifies catalog removal.
func (r *viewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data viewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropViewStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if _, found, err = r.read(ctx, data); err == nil && found {
				resp.Diagnostics.AddError("Delete view", "The view remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete view", err.Error())
	}
}

// ImportState restores the database/schema/name binding from JSON; Read then fills the catalog definition.
func (r *viewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
}
