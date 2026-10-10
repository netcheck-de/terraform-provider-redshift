package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// rlsPolicyResource manages one row-level security policy definition; attachments and the relation's RLS switch
// belong to redshift_rls_policy_attachment and redshift_table_security.
type rlsPolicyResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// rlsPolicyModel is the Terraform state of one RLS policy.
type rlsPolicyModel struct {
	// ID records the warehouse/database/policy identity.
	ID types.String `tfsdk:"id"`
	// Database contains the policy.
	Database types.String `tfsdk:"database"`
	// Name identifies the policy within its database.
	Name types.String `tfsdk:"name"`
	// Column lists the WITH columns the predicate reads from attached relations, one block each.
	Column types.List `tfsdk:"column"`
	// Alias is the relation alias of the WITH clause.
	Alias types.String `tfsdk:"alias"`
	// Predicate is the USING expression as configured, or the catalog text after an outside change or import.
	Predicate types.String `tfsdk:"predicate"`
	// DefinitionFingerprint hashes the catalog predicate to detect changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
}

var _ = registerResource(newRlsPolicyResource)

// newRlsPolicyResource constructs an RLS policy lifecycle handler.
func newRlsPolicyResource() resource.Resource { return &rlsPolicyResource{} }

// Metadata identifies the RLS policy resource to Terraform.
func (r *rlsPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rls_policy"
}

// Schema defines the policy identity, its WITH clause, and the predicate that changes in place.
func (r *rlsPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one row-level security (RLS) policy. Creating a policy filters nothing until it is attached with `redshift_rls_policy_attachment` and row-level security is turned on for the relation with `redshift_table_security`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local database that holds the policy and the relations it is attached to. Changing it replaces the policy.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Policy name, unique within the database. Changing it replaces the policy.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"alias": schema.StringAttribute{
				Optional: true, Computed: true, MarkdownDescription: "Relation alias of the `WITH` clause (`AS alias`), which the predicate may use to qualify columns; requires a `column` block. All policies attached to one relation must use the same alias. It compares without case, as Redshift folds identifiers. Removing it from configuration keeps the current alias. Like `column`, a change needs `replace_triggered_by` on the attachments. Changing it replaces the policy.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), rlsPolicyAliasEquivalent{}, stringplanmodifier.RequiresReplace()},
			},
			"predicate": schema.StringAttribute{
				Required: true, MarkdownDescription: "Filter expression of the `USING ( ... )` clause, applied to the `WHERE` clause of queries on attached relations, for example `region = current_user`. It must be a single expression without `;`. Changing it runs `ALTER RLS POLICY` in place. Redshift stores a rewritten form; when that changes outside Terraform, refresh reports the catalog text.",
			},
			"definition_fingerprint": definitionFingerprintAttribute(),
		},
		Blocks: map[string]schema.Block{
			"column": schema.ListNestedBlock{
				MarkdownDescription: "Ordered `WITH` columns the predicate reads from each attached relation, which must have all of them. Omit the blocks only when the predicate references no relation column. Names compare without case and types in canonical form, so respelling `VARCHAR(64)` as the catalog's `character varying(64)`, or importing the policy, plans no change. `ALTER RLS POLICY` cannot change the `WITH` clause, and `DROP RLS POLICY` refuses a policy that is still attached, so give each attachment `replace_triggered_by` on `column` and `alias`. Changing it replaces the policy.",
				PlanModifiers:       []planmodifier.List{rlsPolicyColumnsEquivalent{}, listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{Required: true, MarkdownDescription: "Column name in the attached relations."},
					"type": schema.StringAttribute{Required: true, MarkdownDescription: "Redshift data type, such as `VARCHAR(64)` or `INTEGER`; compared with the catalog in canonical form."},
				}},
			},
		},
	}
}

// rlsPolicyColumnsEquivalent plans the prior columns when the configuration only respells them. RequiresReplace
// compares raw values, so without it an import (which stores the catalog spelling) or a respelled type would plan
// a replacement that DROP RLS POLICY refuses while the policy is attached. Terraform accepts a planned value equal
// to the prior state for a non-computed attribute of a block element as the provider's statement that both are
// equivalent; equivalent columns have the same count, so the planned blocks still match the configured ones.
type rlsPolicyColumnsEquivalent struct{}

// Description explains the modifier in plain text.
func (rlsPolicyColumnsEquivalent) Description(context.Context) string {
	return "Keeps the prior columns when the configuration differs only in name case or type spelling."
}

// MarkdownDescription explains the modifier in Markdown.
func (m rlsPolicyColumnsEquivalent) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// PlanModifyList keeps the prior value for equivalent known configuration; anything unparsable plans as written.
func (rlsPolicyColumnsEquivalent) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.PlanValue.Equal(req.StateValue) {
		return
	}
	configured, err := rlsPolicyColumns(req.ConfigValue)
	if err == nil && rlsPolicyColumnsMatch(configured, rlsPolicyCatalogColumnsOf(req.StateValue)) {
		resp.PlanValue = req.StateValue
	}
}

// rlsPolicyAliasEquivalent plans the prior alias when the configuration differs only in case, for the same reason
// as rlsPolicyColumnsEquivalent; read already treats such aliases as equal.
type rlsPolicyAliasEquivalent struct{}

// Description explains the modifier in plain text.
func (rlsPolicyAliasEquivalent) Description(context.Context) string {
	return "Keeps the prior alias when the configuration differs only in case."
}

// MarkdownDescription explains the modifier in Markdown.
func (m rlsPolicyAliasEquivalent) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// PlanModifyString keeps the prior alias for a known configuration that matches it without case.
func (rlsPolicyAliasEquivalent) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.EqualFold(req.ConfigValue.ValueString(), req.StateValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// rlsPolicyIdentity is the JSON identity shared by the resource, its lookup, and imports.
func (r *rlsPolicyResource) rlsPolicyIdentity(data rlsPolicyModel) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
}

// read refreshes the policy's columns and alias and returns the catalog predicate, which callers reconcile with
// the configured text. Columns keep their configured spelling while they match the catalog.
func (r *rlsPolicyResource) read(ctx context.Context, data *rlsPolicyModel) (bool, string, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, "", err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, "", err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readRlsPolicyQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, "", err
	}
	if len(rows) != 1 || rows[0]["polname"] == "" {
		return false, "", fmt.Errorf("RLS policy %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
	}
	row := rows[0]
	catalogColumns, err := parseRlsPolicyColumns(row["polatts"])
	if err != nil {
		return false, "", err
	}
	if configured, err := rlsPolicyColumns(data.Column); err != nil || !rlsPolicyColumnsMatch(configured, catalogColumns) {
		data.Column = rlsPolicyColumnsList(catalogColumns)
	}
	if alias := row["polalias"]; !strings.EqualFold(knownString(data.Alias), alias) || data.Alias.IsUnknown() {
		data.Alias = types.StringNull()
		if alias != "" {
			data.Alias = types.StringValue(alias)
		}
	}
	data.Name = types.StringValue(row["polname"])
	return true, row["polqual"], nil
}

// verify re-reads the policy after Create or Update into data and records the catalog fingerprint, failing when
// the catalog does not hold what was planned.
func (r *rlsPolicyResource) verify(ctx context.Context, data *rlsPolicyModel, planned rlsPolicyModel) error {
	found, predicate, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return fmt.Errorf("the RLS policy is absent after the change")
	case !planned.Column.IsUnknown() && !data.Column.Equal(planned.Column):
		return fmt.Errorf("the catalog reports WITH columns %s instead of the planned %s", data.Column, planned.Column)
	case !planned.Alias.IsUnknown() && !data.Alias.Equal(planned.Alias):
		return fmt.Errorf("the catalog reports alias %s instead of the planned %s", data.Alias, planned.Alias)
	}
	data.Predicate, data.DefinitionFingerprint = recordDefinition(planned.Predicate, predicate)
	return nil
}

// ModifyPlan keeps the recorded fingerprint while the predicate is unchanged. The framework marks it unknown as soon
// as the configuration differs from state, which includes a respelled WITH clause that rlsPolicyColumnsEquivalent
// then plans as unchanged; without this, such a respelling would still plan an empty update.
func (r *rlsPolicyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, prior rlsPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || !plan.DefinitionFingerprint.IsUnknown() || !plan.Predicate.Equal(prior.Predicate) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("definition_fingerprint"), prior.DefinitionFingerprint)...)
}

// Create validates the definition before the first state write, creates the policy, and verifies it.
func (r *rlsPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data rlsPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createRlsPolicyStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid RLS policy", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Create RLS policy", err.Error())
		return
	}
	data.ID = r.rlsPolicyIdentity(data)
	planned := data
	// A failed verification must still leave serializable state for the created policy.
	if data.Alias.IsUnknown() {
		data.Alias = types.StringNull()
	}
	data.DefinitionFingerprint = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.verify(ctx, &data, planned); err != nil {
		resp.Diagnostics.AddError("Verify RLS policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ValidateConfig reports definitions Create would reject, during planning once the configuration is known.
func (r *rlsPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data rlsPolicyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createRlsPolicyStatement(data); err != nil {
		resp.Diagnostics.AddError("Invalid RLS policy", err.Error())
	}
}

// Read refreshes the policy, surfaces outside predicate changes, and removes a missing policy from state.
func (r *rlsPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data rlsPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, predicate, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read RLS policy", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	data.Predicate, data.DefinitionFingerprint = reconcileDefinition(data.Predicate, data.DefinitionFingerprint, predicate)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update changes the predicate in place and verifies the result.
func (r *rlsPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior rlsPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(prior.ID, prior.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update RLS policy", err.Error())
		return
	}
	statements, err := alterRlsPolicyStatements(prior, plan)
	if err == nil {
		err = r.exec(ctx, plan.Database.ValueString(), statements...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update RLS policy", err.Error())
		return
	}
	if err := r.verify(ctx, &plan, plan); err != nil {
		resp.Diagnostics.AddError("Verify RLS policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the policy restrictively and verifies catalog removal.
func (r *rlsPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data rlsPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, _, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropRlsPolicyStatement(data))
		if err == nil {
			found, _, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete RLS policy", "The RLS policy remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete RLS policy", err.Error())
	}
}

// ImportState restores the database/name binding from JSON; refresh reads the definition.
func (r *rlsPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}

// rlsPolicyFromRow converts a catalog row for listings, where nothing is configured.
func rlsPolicyFromRow(row sqlclient.Row) (rlsPolicyModel, error) {
	columns, err := parseRlsPolicyColumns(row["polatts"])
	if err != nil {
		return rlsPolicyModel{}, err
	}
	alias := types.StringNull()
	if row["polalias"] != "" {
		alias = types.StringValue(row["polalias"])
	}
	return rlsPolicyModel{
		Database: types.StringValue(row["poldb"]), Name: types.StringValue(row["polname"]), Column: rlsPolicyColumnsList(columns),
		Alias: alias, Predicate: types.StringValue(row["polqual"]), DefinitionFingerprint: types.StringValue(definitionFingerprint(row["polqual"])),
	}, nil
}
