package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// commentResource owns annotation text independently of its target object's definition.
type commentResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// commentModel is the Terraform state for an annotation on one local SQL object.
type commentModel struct {
	// ID records the administration binding and annotated object identity.
	ID types.String `tfsdk:"id"`
	// DatabaseName is the local database where COMMENT executes.
	DatabaseName types.String `tfsdk:"database_name"`
	// ObjectType selects DATABASE, SCHEMA, TABLE, VIEW, COLUMN, or CONSTRAINT.
	ObjectType types.String `tfsdk:"object_type"`
	// ObjectName identifies the database, schema, or parent relation of a column or constraint.
	ObjectName types.String `tfsdk:"object_name"`
	// SchemaName qualifies table, view, column, and constraint targets.
	SchemaName types.String `tfsdk:"schema_name"`
	// ColumnName selects a column annotation within ObjectName.
	ColumnName types.String `tfsdk:"column_name"`
	// ConstraintName selects a constraint annotation on the table ObjectName.
	ConstraintName types.String `tfsdk:"constraint_name"`
	// Text is the annotation; empty text clears it with IS NULL.
	Text types.String `tfsdk:"text"`
}

var _ = registerResource(newCommentResource)

// newCommentResource constructs an annotation handler that never owns its target object.
func newCommentResource() resource.Resource { return &commentResource{} }

// Metadata identifies the comment resource to Terraform.
func (r *commentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_comment"
}

// Schema defines the target object's immutable identity and mutable annotation text.
func (r *commentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages an object's comment independently of its definition. Destroy clears only the annotation.", Attributes: map[string]schema.Attribute{
		"id":              idAttribute(),
		"database_name":   privilegeString("Local database containing the target object. Changing it replaces the comment.", false),
		"object_type":     privilegeString("Kind of the annotated object: `DATABASE`, `SCHEMA`, `TABLE`, `VIEW`, `COLUMN`, or `CONSTRAINT`. Changing it replaces the comment.", false, "DATABASE", "SCHEMA", "TABLE", "VIEW", "COLUMN", "CONSTRAINT"),
		"object_name":     privilegeString("Name of the database or schema itself; for `TABLE` and `VIEW` the relation; for `COLUMN` and `CONSTRAINT` the table that holds it. Changing it replaces the comment.", false),
		"schema_name":     privilegeString("Schema of the relation; required for `TABLE`, `VIEW`, `COLUMN`, and `CONSTRAINT`, and rejected for `DATABASE` and `SCHEMA`. Changing it replaces the comment.", true),
		"column_name":     privilegeString("Column within `object_name`; required for `COLUMN` and rejected otherwise. Changing it replaces the comment.", true),
		"constraint_name": privilegeString("Primary key, unique, or foreign key constraint on the table `object_name`; required for `CONSTRAINT` and rejected otherwise. Changing it replaces the comment.", true),
		"text":            schema.StringAttribute{Required: true, MarkdownDescription: "Comment text. An empty string represents no annotation and clears it with `IS NULL`."},
	}}
}

// read checks local target existence and retrieves its annotation from the target database.
func (r *commentResource) read(ctx context.Context, data *commentModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	query, err := readCommentQuery(*data)
	if err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), commentDatabaseQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	rows, err = r.selectRows(ctx, data.DatabaseName.ValueString(), query)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("comment target catalog identity is ambiguous")
	}
	data.Text = types.StringValue(rows[0]["text"])
	return true, nil
}

// reconcile changes only annotation text and verifies its catalog value, including removal.
func (r *commentResource) reconcile(ctx context.Context, data commentModel, deleting bool) error {
	actual := data
	found, err := r.read(ctx, &actual)
	if err != nil {
		return err
	}
	if !found {
		if deleting {
			return nil
		}
		return fmt.Errorf("comment target does not exist")
	}
	desired := data.Text.ValueString()
	if deleting {
		desired = ""
	}
	if actual.Text.ValueString() == desired {
		return nil
	}
	statement, err := commentStatement(data, desired)
	if err != nil {
		return err
	}
	if err := r.exec(ctx, data.DatabaseName.ValueString(), statement); err != nil {
		return err
	}
	found, err = r.read(ctx, &actual)
	if err == nil && (!found || actual.Text.ValueString() != desired) {
		return fmt.Errorf("comment did not converge")
	}
	return err
}

// Create records annotation ownership and sets the requested text without creating its target.
func (r *commentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid targets must fail before ownership is recorded, or Read and Delete could never succeed.
	if _, _, err := commentTarget(data); err != nil {
		resp.Diagnostics.AddError("Create comment", err.Error())
		return
	}
	fields := map[string]string{"database_name": data.DatabaseName.ValueString(), "object_type": data.ObjectType.ValueString(), "object_name": data.ObjectName.ValueString()}
	if !data.SchemaName.IsNull() {
		fields["schema_name"] = data.SchemaName.ValueString()
	}
	if !data.ColumnName.IsNull() {
		fields["column_name"] = data.ColumnName.ValueString()
	}
	if !data.ConstraintName.IsNull() {
		fields["constraint_name"] = data.ConstraintName.ValueString()
	}
	data.ID = r.identity(r.database.ValueString(), fields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data, false); err != nil {
		resp.Diagnostics.AddError("Create comment", err.Error())
	}
}

// ValidateConfig reports invalid comment targets during planning once the configuration is known.
func (r *commentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, _, err := commentTarget(data); err != nil {
		resp.Diagnostics.AddError("Invalid comment target", err.Error())
	}
}

// Read refreshes comment text or removes state when the target object is absent.
func (r *commentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read comment", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update reconciles annotation text under the existing object binding.
func (r *commentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, false); err != nil {
		resp.Diagnostics.AddError("Update comment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete clears the annotation with IS NULL while preserving the database object.
func (r *commentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, true); err != nil {
		resp.Diagnostics.AddError("Delete comment", err.Error())
	}
}

// ImportState restores comment target identity; refresh retrieves the current text.
func (r *commentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database_name", "object_type", "object_name")
	if resp.Diagnostics.HasError() {
		return
	}
	var values map[string]string
	_ = json.Unmarshal([]byte(req.ID), &values)
	for _, field := range []string{"schema_name", "column_name", "constraint_name"} {
		if values[field] != "" {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(field), values[field])...)
		}
	}
}
