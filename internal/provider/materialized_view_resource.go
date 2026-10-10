package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// materializedViewResource manages a materialized view stored in Redshift.
type materializedViewResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// materializedViewModel is the Terraform state of one materialized view.
type materializedViewModel struct {
	// ID records the warehouse/database/schema/name identity.
	ID types.String `tfsdk:"id"`
	// Database contains the materialized view.
	Database types.String `tfsdk:"database"`
	// Schema contains the materialized view.
	Schema types.String `tfsdk:"schema"`
	// Name identifies the materialized view within its schema.
	Name types.String `tfsdk:"name"`
	// Query is the configured SELECT, or the catalog definition after a change made outside Terraform.
	Query types.String `tfsdk:"query"`
	// Backup selects BACKUP YES or NO; null leaves the server default.
	Backup types.Bool `tfsdk:"backup"`
	// Distribution is the configured distribution block, with style and key; null when the block is absent.
	Distribution types.Object `tfsdk:"distribution"`
	// SortKey is the configured compound sort key block, with its columns in order; null when the block is absent.
	SortKey types.Object `tfsdk:"sort_key"`
	// AutoRefresh selects AUTO REFRESH YES or NO.
	AutoRefresh types.Bool `tfsdk:"auto_refresh"`
	// Owner is the materialized view's SQL owner.
	Owner types.String `tfsdk:"owner"`
	// DefinitionFingerprint detects definition changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
}

var _ = registerResource(newMaterializedViewResource)

// newMaterializedViewResource constructs a materialized view lifecycle handler.
func newMaterializedViewResource() resource.Resource { return &materializedViewResource{} }

// Metadata identifies the materialized view resource to Terraform.
func (r *materializedViewResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_materialized_view"
}

// materializedViewAdoptionNote explains why an import does not replace the view for the options without an ALTER
// form.
const materializedViewAdoptionNote = "An import leaves it unset; the first apply after an import records the configured value without replacing the view " +
	"and warns with the catalog definition, which it cannot compare."

// materializedViewImportedKey marks private state written by ImportState until the first update adopts the
// configuration. Only then is a null query or backup unknown rather than absent from CREATE.
const materializedViewImportedKey = "materialized_view_imported"

// materializedViewPrivate is the private state accessor of plan modifier, import, and update requests.
type materializedViewPrivate interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

// materializedViewReplaces reports whether a changed query or backup replaces the view: always, except for
// a null prior right after an import, which cannot read the option back, so adopting the configured value must
// not recreate the view. A null prior on a view Terraform created means the option was omitted from CREATE.
func materializedViewReplaces(ctx context.Context, state interface{ IsNull() bool }, private materializedViewPrivate) bool {
	if !state.IsNull() || private == nil {
		return true
	}
	imported, _ := private.GetKey(ctx, materializedViewImportedKey)
	return len(imported) == 0
}

// materializedViewReplaceDescription documents the replacement rule in plan output.
const materializedViewReplaceDescription = "Changing it replaces the materialized view, except when adopting the configuration after an import."

// materializedViewReplaceString is materializedViewReplaces for the query.
func materializedViewReplaceString() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = materializedViewReplaces(ctx, req.StateValue, req.Private)
	}, materializedViewReplaceDescription, materializedViewReplaceDescription)
}

// Schema defines the materialized view identity, its creation-only query and backup, and its in-place settings.
func (r *materializedViewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a materialized view stored in Redshift. Refreshing its data is an action and is not managed.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": viewNameAttribute("Local database containing the materialized view. Changing it replaces the view."),
			"schema":   viewNameAttribute("Existing schema containing the materialized view. Changing it replaces the view."),
			"name":     viewNameAttribute("Materialized view name. Changing it replaces the view."),
			"query": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "SELECT statement defining the materialized view, without a trailing `;`. Redshift cannot alter the definition. " +
					"Changing it replaces the view. State keeps this text while the catalog definition is unchanged; a change made " +
					"outside Terraform surfaces the catalog definition and plans a replacement. " + materializedViewAdoptionNote,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{materializedViewReplaceString()},
			},
			"backup": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Include the materialized view in snapshots (`BACKUP YES`) or not (`BACKUP NO`); unset uses the server " +
					"default, `YES`. Not reported by a documented catalog view, so it is not read back. Changing it replaces the view. " + materializedViewAdoptionNote,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = materializedViewReplaces(ctx, req.StateValue, req.Private)
				}, materializedViewReplaceDescription, materializedViewReplaceDescription)},
			},
			"auto_refresh": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Refresh automatically when base tables change (`AUTO REFRESH YES`). Changed in place with `ALTER MATERIALIZED VIEW`. " +
					"Redshift rejects it for queries with mutable functions, external tables, or other materialized views. Defaults to `false`.",
			},
			"owner":                  materializedViewOwnerAttribute(),
			"definition_fingerprint": definitionFingerprintAttribute(),
		},
		Blocks: map[string]schema.Block{
			"distribution": schema.SingleNestedBlock{
				MarkdownDescription: "Distribution of the rows across the compute nodes; omitted uses the server default, `EVEN`. Changed in place " +
					"with `ALTER MATERIALIZED VIEW ... ALTER DISTSTYLE`; removing the block returns the view to `EVEN`. Not read back, so " +
					"changes made outside Terraform are not detected.",
				Attributes: map[string]schema.Attribute{
					"style": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Distribution style: `EVEN`, `ALL`, or `KEY`. Omitted, it is `KEY` when `key` is set. `KEY` requires `key`.",
						Validators:          []validator.String{stringvalidator.OneOf("EVEN", "ALL", "KEY")},
					},
					"key": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Distribution key column; implies `KEY` distribution. Changed with `ALTER DISTSTYLE KEY DISTKEY`.",
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
				},
			},
			"sort_key": schema.SingleNestedBlock{
				MarkdownDescription: "Compound sort key. Changed in place with `ALTER MATERIALIZED VIEW ... ALTER COMPOUND SORTKEY`; removing the " +
					"block runs `ALTER SORTKEY NONE`. Not read back, so changes made outside Terraform are not detected.",
				Attributes: map[string]schema.Attribute{
					// materializedViewAttributes requires it in a present block; see there why the schema cannot.
					"columns": schema.ListAttribute{
						Optional: true, ElementType: types.StringType,
						MarkdownDescription: "Sort key columns in order; required in the block.",
						Validators:          []validator.List{listvalidator.SizeAtLeast(1), listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
					},
				},
			},
		},
	}
}

// materializedViewOwnerAttribute adds the SVV_MV_INFO visibility rule to the shared owner attribute, because a
// transfer away from a regular provider identity hides the refresh setting from it.
func materializedViewOwnerAttribute() schema.StringAttribute {
	owner := viewOwnerAttribute("materialized view")
	owner.MarkdownDescription += " `SVV_MV_INFO` shows a regular user only the materialized views it owns, so after a provider " +
		"identity that is not a superuser transfers ownership away, `auto_refresh` is no longer read back, and changing or " +
		"dropping the view needs privileges that the new owner grants."
	return owner
}

// materializedViewCatalog is the observed owner, definition, and refresh setting.
type materializedViewCatalog struct {
	// view is the pg_views entry.
	view viewCatalogEntry
	// autoRefresh is the SVV_MV_INFO setting; meaningless while refreshHidden is set.
	autoRefresh bool
	// refreshHidden reports that SVV_MV_INFO has no row for a view pg_views lists, which the reference documents
	// for a regular user that does not own the view.
	refreshHidden bool
}

// warnHidden explains why auto_refresh was not read back, so a hidden row is not mistaken for a missing view.
func (observed materializedViewCatalog) warnHidden(diagnostics *diag.Diagnostics, data materializedViewModel) {
	if observed.refreshHidden {
		diagnostics.AddWarning("Materialized view refresh setting not visible", fmt.Sprintf(
			"SVV_MV_INFO shows regular users only the materialized views they own, and %s.%s is owned by %q, so auto_refresh "+
				"keeps its configured or previous value instead of the catalog setting.",
			data.Schema.ValueString(), data.Name.ValueString(), observed.view.owner))
	}
}

// read checks the binding and reads the materialized view from pg_views and SVV_MV_INFO.
func (r *materializedViewResource) read(ctx context.Context, data materializedViewModel) (materializedViewCatalog, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return materializedViewCatalog{}, false, err
	}
	entry, found, err := readViewCatalog(ctx, &r.resourceClient, data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString())
	if err != nil || !found {
		return materializedViewCatalog{}, false, err
	}
	if !entry.materialized {
		return materializedViewCatalog{}, false, errors.New("the relation is an ordinary view; manage it with redshift_view")
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readMaterializedViewRefreshQuery(data))
	if err != nil {
		return materializedViewCatalog{}, false, err
	}
	// pg_views decides existence; SVV_MV_INFO shows regular users only their own materialized views.
	switch len(rows) {
	case 0:
		return materializedViewCatalog{view: entry, refreshHidden: true}, true, nil
	case 1:
	default:
		return materializedViewCatalog{}, false, fmt.Errorf("materialized view %s.%s has ambiguous SVV_MV_INFO rows", data.Schema.ValueString(), data.Name.ValueString())
	}
	autoRefresh, err := materializedViewFlag(rows[0]["autorefresh"])
	if err != nil {
		return materializedViewCatalog{}, false, err
	}
	return materializedViewCatalog{view: entry, autoRefresh: autoRefresh}, true, nil
}

// observe copies the catalog owner and, when visible, the refresh setting into data.
func (data *materializedViewModel) observe(observed materializedViewCatalog) {
	data.Owner = types.StringValue(observed.view.owner)
	if !observed.refreshHidden {
		data.AutoRefresh = types.BoolValue(observed.autoRefresh)
	}
}

// converge re-reads the materialized view after apply, checks that the refresh setting and a configured owner
// took effect, and records the configured query with the fingerprint of the catalog definition. A refresh setting
// SVV_MV_INFO hides cannot be verified and only warns.
func (r *materializedViewResource) converge(ctx context.Context, data *materializedViewModel, planned materializedViewModel, diagnostics *diag.Diagnostics) error {
	observed, found, err := r.read(ctx, *data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the materialized view is absent after apply")
	case !observed.refreshHidden && observed.autoRefresh != planned.AutoRefresh.ValueBool():
		return fmt.Errorf("the materialized view's auto refresh is %t after apply, expected %t", observed.autoRefresh, planned.AutoRefresh.ValueBool())
	case knownString(planned.Owner) != "" && observed.view.owner != planned.Owner.ValueString():
		return fmt.Errorf("the materialized view is owned by %q after apply, expected %q", observed.view.owner, planned.Owner.ValueString())
	}
	observed.warnHidden(diagnostics, *data)
	data.observe(observed)
	data.Query, data.DefinitionFingerprint = recordDefinition(planned.Query, observed.view.definition)
	return nil
}

// Create validates the definition, creates the materialized view, applies a configured owner, and verifies it.
func (r *materializedViewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data materializedViewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := createMaterializedViewStatements(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid materialized view", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements[0]); err != nil {
		resp.Diagnostics.AddError("Create materialized view", err.Error())
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
		resp.Diagnostics.AddError("Set materialized view owner", err.Error())
		return
	}
	if err := r.converge(ctx, &data, planned, &resp.Diagnostics); err != nil {
		resp.Diagnostics.AddError("Verify materialized view", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ValidateConfig reports a definition or storage option Create would reject. The storage blocks are checked even
// while other attributes are unknown, because their rendering skips unknown values, so an empty block fails at
// plan time instead of at apply; the query and name checks wait for a fully known configuration.
func (r *materializedViewResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data materializedViewModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := materializedViewAttributes(data); err != nil {
		resp.Diagnostics.AddError("Invalid materialized view", err.Error())
		return
	}
	if !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createMaterializedViewStatements(data); err != nil {
		resp.Diagnostics.AddError("Invalid materialized view", err.Error())
	}
}

// Read refreshes the owner and refresh setting, surfaces an outside definition change, or removes a missing view.
// After an import no query is configured yet, so state keeps it null instead of the catalog text, which would
// otherwise differ from the configuration and plan a replacement of the imported view.
func (r *materializedViewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data materializedViewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read materialized view", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	observed.warnHidden(&resp.Diagnostics, data)
	data.observe(observed)
	if data.Query.IsNull() {
		data.DefinitionFingerprint = types.StringValue(definitionFingerprint(observed.view.definition))
	} else {
		data.Query, data.DefinitionFingerprint = reconcileDefinition(data.Query, data.DefinitionFingerprint, observed.view.definition)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update changes the storage options, refresh setting, and owner in place. A null prior query or backup from an
// import is adopted from configuration without SQL, with a warning that shows the catalog definition, because
// neither can be compared with the existing view.
func (r *materializedViewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous materializedViewModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := alterMaterializedViewStatements(previous, data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid materialized view", err.Error())
		return
	}
	current, found, err := r.read(ctx, previous)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read materialized view", err.Error())
		return
	case !found:
		resp.Diagnostics.AddError("Update materialized view", "The materialized view disappeared during the update; refresh the plan.")
		return
	}
	if previous.Query.IsNull() {
		resp.Diagnostics.AddWarning("Imported materialized view adopted without verification", fmt.Sprintf(
			"Terraform recorded the configured query and backup without comparing them to the imported materialized view: "+
				"Redshift reports no backup setting and prints the definition in its own formatting. Storage options left unset "+
				"were not checked either. If the catalog definition below does not match the configuration, replace the view "+
				"with `terraform apply -replace`.\n\n%s", current.view.definition))
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update materialized view", err.Error())
		return
	}
	if err := r.converge(ctx, &data, data, &resp.Diagnostics); err != nil {
		resp.Diagnostics.AddError("Verify materialized view", err.Error())
		return
	}
	// State now holds the configured query and backup, so later changes to them replace the view again.
	if resp.Private != nil {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, materializedViewImportedKey, nil)...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the materialized view restrictively and verifies catalog removal.
func (r *materializedViewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data materializedViewModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropMaterializedViewStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if _, found, err = r.read(ctx, data); err == nil && found {
				resp.Diagnostics.AddError("Delete materialized view", "The materialized view remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete materialized view", err.Error())
	}
}

// ImportState restores the database/schema/name binding from JSON and marks the state as imported, so the first
// update adopts the configured query and backup; Read then fills the observed settings.
func (r *materializedViewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
	// The framework always initializes Private; harnesses that call ImportState directly may not.
	if !resp.Diagnostics.HasError() && resp.Private != nil {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, materializedViewImportedKey, []byte("true"))...)
	}
}
