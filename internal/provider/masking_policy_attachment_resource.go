package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// maskingPolicyAttachmentResource attaches one masking policy to columns of a relation for one recipient.
type maskingPolicyAttachmentResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// maskingPolicyAttachmentModel is the Terraform state of one attachment.
type maskingPolicyAttachmentModel struct {
	// ID records the warehouse, database, policy, relation, columns, and recipient.
	ID types.String `tfsdk:"id"`
	// Database holds both the policy and the relation.
	Database types.String `tfsdk:"database"`
	// Policy names the attached masking policy.
	Policy types.String `tfsdk:"policy"`
	// Schema qualifies the relation.
	Schema types.String `tfsdk:"schema"`
	// Relation is the table or view whose columns are masked.
	Relation types.String `tfsdk:"relation"`
	// Columns are the masked output columns, in policy output order.
	Columns types.List `tfsdk:"columns"`
	// InputColumns are the relation columns fed to the policy inputs; the catalog reports Columns when omitted.
	InputColumns types.List `tfsdk:"input_columns"`
	// Grantee names the user or role, or public.
	Grantee types.String `tfsdk:"grantee"`
	// GranteeType is USER, ROLE, or PUBLIC.
	GranteeType types.String `tfsdk:"grantee_type"`
	// Priority decides which of several applicable policies masks a column.
	Priority types.Int64 `tfsdk:"priority"`
}

var _ = registerResource(newMaskingPolicyAttachmentResource)

// newMaskingPolicyAttachmentResource constructs a masking policy attachment handler.
func newMaskingPolicyAttachmentResource() resource.Resource {
	return &maskingPolicyAttachmentResource{}
}

// Metadata identifies the attachment resource to Terraform.
func (r *maskingPolicyAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_masking_policy_attachment"
}

// maskingAttachmentString defines an immutable nonempty identity string.
func maskingAttachmentString(description string, choices ...string) schema.StringAttribute {
	validators := []validator.String{stringvalidator.LengthAtLeast(1)}
	if len(choices) != 0 {
		validators = append(validators, stringvalidator.OneOf(choices...))
	}
	return schema.StringAttribute{
		Required: true, MarkdownDescription: description, Validators: validators,
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

// Schema defines the attachment identity, its optional input mapping, and its in-place priority.
func (r *maskingPolicyAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	nonEmpty := []validator.List{listvalidator.SizeAtLeast(1), listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a dynamic data masking policy to columns of one relation for one user, role, or `PUBLIC`. Attaching a policy does not need any table-level setting; Redshift masks the columns for the recipient right away.",
		Attributes: map[string]schema.Attribute{
			"id":           idAttribute(),
			"database":     maskingAttachmentString("Local database that holds both the policy and the relation. Changing it replaces the attachment."),
			"policy":       maskingAttachmentString("Masking policy to attach, such as `redshift_masking_policy.example.name`. Changing it replaces the attachment."),
			"schema":       maskingAttachmentString("Schema of the relation. Changing it replaces the attachment."),
			"relation":     maskingAttachmentString("Table or view whose columns are masked. Changing it replaces the attachment."),
			"grantee":      maskingAttachmentString("User or role the policy applies to; use `public` with `grantee_type = \"PUBLIC\"`. Changing it replaces the attachment."),
			"grantee_type": maskingAttachmentString("`USER`, `ROLE`, or `PUBLIC`. Changing it replaces the attachment.", "USER", "ROLE", "PUBLIC"),
			"columns": schema.ListAttribute{
				Required: true, ElementType: types.StringType, Validators: nonEmpty,
				MarkdownDescription: "Ordered output columns that the policy masks, one per policy output. Changing it replaces the attachment.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"input_columns": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType, Validators: nonEmpty,
				MarkdownDescription: "Ordered relation columns passed to the policy's input columns (`USING`). Defaults to `columns`, as Redshift does. Changing it replaces the attachment.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown(), listplanmodifier.RequiresReplace()},
			},
			"priority": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				MarkdownDescription: "Priority among the policies that apply to a column; the highest wins, and two different policies on one column cannot share a priority even for different recipients. Defaults to `0`. Redshift has no command to change it, so an update first checks that no other policy holds the new priority on these columns, then detaches and re-attaches the policy: until the second statement completes, the recipient sees the column under the next applicable policy, or unmasked if there is none. If the re-attach fails, the update restores the previous attachment and reports the error.",
			},
		},
	}
}

// read refreshes the attachment's inputs and priority from the catalog; a missing database or row is not found.
func (r *maskingPolicyAttachmentResource) read(ctx context.Context, data *maskingPolicyAttachmentModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readMaskingAttachmentQuery(*data))
	if err != nil {
		return false, err
	}
	row, err := maskingAttachmentRow(*data, rows)
	if err != nil || row == nil {
		return false, err
	}
	priority, err := maskingAttachmentPriority(row["priority"])
	if err != nil {
		return false, err
	}
	inputs, err := maskingAttachmentParseNames(row["input_columns"])
	if err != nil {
		return false, err
	}
	// Keep the configured spelling when the catalog only folded the case.
	if !maskingAttachmentNamesMatch(maskingAttachmentNames(data.InputColumns), inputs) {
		data.InputColumns = maskingAttachmentNameList(inputs)
	}
	data.Priority = types.Int64Value(priority)
	return true, nil
}

// verify re-reads the attachment after Create or Update and checks that it holds the planned values.
func (r *maskingPolicyAttachmentResource) verify(ctx context.Context, data *maskingPolicyAttachmentModel) error {
	planned := *data
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return fmt.Errorf("the masking policy attachment is absent after the change")
	case !planned.Priority.IsUnknown() && !data.Priority.Equal(planned.Priority):
		return fmt.Errorf("the masking policy attachment has priority %d instead of %d", data.Priority.ValueInt64(), planned.Priority.ValueInt64())
	case !planned.InputColumns.IsNull() && !planned.InputColumns.IsUnknown() && !data.InputColumns.Equal(planned.InputColumns):
		return fmt.Errorf("the masking policy attachment's input columns differ from the configuration")
	}
	return nil
}

// ValidateConfig reports invalid tuples during planning once the configuration is known.
func (r *maskingPolicyAttachmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data maskingPolicyAttachmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := maskingAttachmentValidate(data); err != nil {
		resp.Diagnostics.AddError("Invalid masking policy attachment", err.Error())
	}
}

// Create attaches the policy and verifies the attachment in the catalog.
func (r *maskingPolicyAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data maskingPolicyAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := attachMaskingPolicyStatement(data)
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create masking policy attachment", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), maskingAttachmentIdentity(data))
	planned := data
	// A failed verification must still record the attachment with serializable state.
	if data.InputColumns.IsUnknown() {
		data.InputColumns = types.ListNull(types.StringType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	data.InputColumns = planned.InputColumns
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify masking policy attachment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the attachment or removes it when it, its policy, relation, or database is gone.
func (r *maskingPolicyAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data maskingPolicyAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read masking policy attachment", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update moves the attachment from what the catalog holds to the planned priority and verifies it.
func (r *maskingPolicyAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior maskingPolicyAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = prior.ID
	// Comparing with the catalog rather than the prior state also repairs a priority changed outside Terraform.
	current := plan
	found, err := r.read(ctx, &current)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read masking policy attachment", err.Error())
		return
	case !found:
		resp.Diagnostics.AddError("Update masking policy attachment", "The attachment disappeared during the update; refresh the plan.")
		return
	}
	statements, err := alterMaskingAttachmentStatements(current, plan)
	if err == nil && len(statements) != 0 {
		err = r.checkPriority(ctx, plan)
	}
	if err == nil {
		err = r.reattach(ctx, current, statements)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update masking policy attachment", err.Error())
		return
	}
	if err := r.verify(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Verify masking policy attachment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// checkPriority fails before any DETACH when another policy already holds the planned priority on a planned column.
func (r *maskingPolicyAttachmentResource) checkPriority(ctx context.Context, plan maskingPolicyAttachmentModel) error {
	peers, err := r.selectRows(ctx, plan.Database.ValueString(), maskingAttachmentPeersQuery(plan))
	if err != nil {
		return err
	}
	return maskingAttachmentPriorityConflict(plan, peers)
}

// reattach runs the DETACH-then-ATTACH statements of an update. They cannot share a transaction, so when a statement
// after the DETACH fails, the attachment the catalog held is restored before the error is reported; otherwise the
// recipient would read the columns unmasked until a later apply re-created the attachment.
func (r *maskingPolicyAttachmentResource) reattach(ctx context.Context, current maskingPolicyAttachmentModel, statements []string) error {
	database := current.Database.ValueString()
	for i, statement := range statements {
		err := r.exec(ctx, database, statement)
		if err == nil {
			continue
		}
		if i == 0 {
			return err
		}
		restore, restoreErr := attachMaskingPolicyStatement(current)
		if restoreErr == nil {
			restoreErr = r.exec(ctx, database, restore)
		}
		if restoreErr != nil {
			return fmt.Errorf("%w; restoring the previous attachment also failed, so the columns are unmasked for the recipient until the next apply: %w", err, restoreErr)
		}
		return fmt.Errorf("%w; the previous attachment with priority %d was restored", err, current.Priority.ValueInt64())
	}
	return nil
}

// Delete detaches the policy for this recipient and columns and verifies the removal.
func (r *maskingPolicyAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data maskingPolicyAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		var statement string
		if statement, err = detachMaskingPolicyStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				err = fmt.Errorf("the masking policy attachment remains after deletion")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete masking policy attachment", err.Error())
	}
}

// ImportState restores the attachment tuple from the same JSON identity Create records; columns is a JSON list.
func (r *maskingPolicyAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "policy", "schema", "relation", "grantee", "grantee_type")
	if resp.Diagnostics.HasError() {
		return
	}
	var values map[string]string
	var columns []string
	// importIdentity already rejected malformed JSON.
	_ = json.Unmarshal([]byte(req.ID), &values)
	if err := json.Unmarshal([]byte(values["columns"]), &columns); err != nil || len(columns) == 0 {
		resp.Diagnostics.AddError("Invalid import identity", `Field columns must be a nonempty JSON list of column names, such as "[\"email\"]".`)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("columns"), maskingAttachmentNameList(columns))...)
}

// maskingAttachmentModelFromObject reads the selector attributes of a lookup into a model.
func maskingAttachmentModelFromObject(data types.Object) maskingPolicyAttachmentModel {
	attributes := data.Attributes()
	return maskingPolicyAttachmentModel{
		ID: types.StringNull(), Database: attributes["database"].(types.String), Policy: attributes["policy"].(types.String),
		Schema: attributes["schema"].(types.String), Relation: attributes["relation"].(types.String),
		Columns: attributes["columns"].(types.List), InputColumns: types.ListNull(types.StringType),
		Grantee: attributes["grantee"].(types.String), GranteeType: attributes["grantee_type"].(types.String),
		Priority: types.Int64Null(),
	}
}
