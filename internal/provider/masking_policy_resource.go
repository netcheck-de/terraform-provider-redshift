package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// maskingPolicyResource manages one dynamic data masking policy.
type maskingPolicyResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// maskingPolicyModel is the Terraform state of a masking policy.
type maskingPolicyModel struct {
	// ID records the warehouse/database/policy identity.
	ID types.String `tfsdk:"id"`
	// Database is the local database that holds the policy.
	Database types.String `tfsdk:"database"`
	// Name identifies the policy within its database.
	Name types.String `tfsdk:"name"`
	// InputColumns are the typed inputs of the masking expression, in order.
	InputColumns types.List `tfsdk:"input_columns"`
	// Expression is the configured masking expression; Read replaces it with the catalog text after drift.
	Expression types.String `tfsdk:"expression"`
	// DefinitionFingerprint hashes the catalog expression to detect changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
}

var _ = registerResource(newMaskingPolicyResource)

// newMaskingPolicyResource constructs a masking policy lifecycle handler.
func newMaskingPolicyResource() resource.Resource { return &maskingPolicyResource{} }

// Metadata identifies the masking policy resource to Terraform.
func (r *maskingPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_masking_policy"
}

// Schema defines the policy identity, its typed inputs, and its in-place expression.
func (r *maskingPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one dynamic data masking policy: its typed input columns and the masking expression. Attach it to columns with `redshift_masking_policy_attachment`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, Validators: nonEmpty,
				MarkdownDescription: "Local database that holds the policy; the policy can only be attached to relations in this database. Changing it replaces the policy.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, Validators: nonEmpty,
				MarkdownDescription: "Policy name, unique among the masking policies of the database. Changing it replaces the policy.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"input_columns": schema.ListNestedAttribute{
				Required: true, Validators: []validator.List{listvalidator.SizeAtLeast(1)},
				MarkdownDescription: "Ordered input columns of the `WITH` clause that the expression reads. Their types must match the masked columns' types when the policy is attached. Redshift cannot alter them, so changing a name or type replaces the policy; another spelling of the same type, such as `TEXT` for `VARCHAR(256)`, does not.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplaceIf(maskingPolicyInputsReplacement,
					"Replaces the policy when an input column's name or type changes.", "Replaces the policy when an input column's name or type changes.")},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{Required: true, Validators: nonEmpty, MarkdownDescription: "Input column name used in `expression`; it does not have to match the masked column's name."},
					"type": schema.StringAttribute{Required: true, Validators: nonEmpty, MarkdownDescription: "Redshift data type, such as `VARCHAR(256)` or `INTEGER`. A type without length gets the length a column would get, so `VARCHAR` means `VARCHAR(256)`."},
				}},
			},
			"expression": schema.StringAttribute{
				Required: true, Validators: nonEmpty,
				MarkdownDescription: "SQL expression of the `USING` clause that computes the masked value from the input columns, for example `'XXXX'::VARCHAR(256)` or a `CASE` over the inputs. A constant must be cast to the input type. Updated in place with `ALTER MASKING POLICY`. Redshift stores its own rendering, so refresh keeps the configured text while `definition_fingerprint` matches and shows the catalog text after a change made outside Terraform.",
			},
			"definition_fingerprint": definitionFingerprintAttribute(),
		},
	}
}

// maskingPolicyInputsReplacement replaces the policy only when the inputs really change. The catalog spells types
// its own way, so after an import the state holds character varying(256) where the configuration says VARCHAR(256);
// that difference plans an in-place update, which renders no statement and keeps the configured spelling. Unknown
// inputs compare as different, so a value known only at apply time still replaces the policy.
func maskingPolicyInputsReplacement(ctx context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	plan, err := req.PlanValue.ToTerraformValue(ctx)
	resp.RequiresReplace = err != nil || !plan.IsFullyKnown() ||
		!maskingPolicyColumnsMatch(maskingPolicyColumns(req.PlanValue), maskingPolicyColumns(req.StateValue))
}

// read refreshes the policy's inputs and returns the catalog expression; a missing database or policy is not found.
func (r *maskingPolicyResource) read(ctx context.Context, data *maskingPolicyModel) (string, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return "", false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return "", false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readMaskingPolicyQuery(*data))
	if err != nil || len(rows) == 0 {
		return "", false, err
	}
	if len(rows) != 1 {
		return "", false, fmt.Errorf("masking policy %q has ambiguous catalog metadata", data.Name.ValueString())
	}
	columns, err := maskingPolicyParseColumns(rows[0]["input_columns"])
	if err != nil {
		return "", false, err
	}
	// Keep the configured spelling of equivalent types, so VARCHAR(256) does not drift to character varying(256).
	if !maskingPolicyColumnsMatch(maskingPolicyColumns(data.InputColumns), columns) {
		data.InputColumns = maskingPolicyColumnList(columns)
	}
	return maskingPolicyExpressionText(rows[0]["policy_expression"]), true, nil
}

// verify re-reads the policy after Create or Update and records the catalog fingerprint of the configured text.
func (r *maskingPolicyResource) verify(ctx context.Context, data *maskingPolicyModel) error {
	planned := data.InputColumns
	text, found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return fmt.Errorf("the masking policy is absent after the change")
	case !data.InputColumns.Equal(planned):
		return fmt.Errorf("the masking policy's input columns differ from the configuration")
	}
	data.Expression, data.DefinitionFingerprint = recordDefinition(data.Expression, text)
	return nil
}

// ValidateConfig reports invalid inputs and expressions during planning once the configuration is known.
func (r *maskingPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data maskingPolicyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := maskingPolicyValidate(data); err != nil {
		resp.Diagnostics.AddError("Invalid masking policy", err.Error())
	}
}

// Create creates the policy and verifies it in the catalog.
func (r *maskingPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data maskingPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createMaskingPolicyStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Create masking policy", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Create masking policy", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// A failed verification must still record the created policy with serializable state.
	data.DefinitionFingerprint = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify masking policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the policy or removes it from state when it or its database is gone.
func (r *maskingPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data maskingPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	text, found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read masking policy", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	data.Expression, data.DefinitionFingerprint = reconcileDefinition(data.Expression, data.DefinitionFingerprint, text)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update alters the expression in place and verifies the policy.
func (r *maskingPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior maskingPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Check ownership before any statement runs; verify would only notice after the ALTER.
	if err := r.bound(prior.ID, plan.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update masking policy", err.Error())
		return
	}
	statements, err := alterMaskingPolicyStatements(prior, plan)
	if err == nil {
		err = r.exec(ctx, plan.Database.ValueString(), statements...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update masking policy", err.Error())
		return
	}
	plan.ID = prior.ID
	if err := r.verify(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Verify masking policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the policy and verifies its removal; Redshift refuses while it is still attached.
func (r *maskingPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data maskingPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropMaskingPolicyStatement(data))
		if err == nil {
			_, found, err = r.read(ctx, &data)
			if err == nil && found {
				err = fmt.Errorf("the masking policy remains after deletion")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete masking policy", err.Error())
	}
}

// ImportState restores the policy's database and name from its JSON identity; Read fills in the definition.
func (r *maskingPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
