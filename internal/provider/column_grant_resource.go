package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// columnGrantResource owns the exact column-level SELECT/UPDATE privileges of one grantee on one table or view.
type columnGrantResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// columnGrantModel is the Terraform state of one relation/grantee tuple.
type columnGrantModel struct {
	// ID records the administration binding and tuple.
	ID types.String `tfsdk:"id"`
	// DatabaseName is the local database holding the relation.
	DatabaseName types.String `tfsdk:"database_name"`
	// SchemaName is the relation's schema.
	SchemaName types.String `tfsdk:"schema_name"`
	// ObjectName is the table or view.
	ObjectName types.String `tfsdk:"object_name"`
	// Grantee is the receiving identity; public for PUBLIC.
	Grantee types.String `tfsdk:"grantee"`
	// GranteeType is ROLE, USER, GROUP, or PUBLIC.
	GranteeType types.String `tfsdk:"grantee_type"`
	// Privileges maps SELECT and UPDATE to the columns that hold them.
	Privileges types.Map `tfsdk:"privileges"`
}

// columnGrantFields are the identity attributes persisted in the JSON import ID.
var columnGrantFields = []string{"database_name", "schema_name", "object_name", "grantee", "grantee_type"}

var _ = registerResource(newColumnGrantResource)

// newColumnGrantResource constructs the column-level grant resource.
func newColumnGrantResource() resource.Resource { return &columnGrantResource{} }

// Metadata identifies the column grant resource to Terraform.
func (r *columnGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_column_grant"
}

// Schema defines the relation/grantee identity and the privilege-to-columns map.
func (r *columnGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id":            idAttribute(),
		"database_name": privilegeString("Local database containing the table or view. Changing it replaces the grant.", false),
		"schema_name":   privilegeString("Schema containing the table or view. Changing it replaces the grant.", false),
		"object_name":   privilegeString("Table, view, or materialized view whose columns are granted; late-binding views do not support column privileges. Changing it replaces the grant.", false),
		"privileges": schema.MapAttribute{
			Required: true, ElementType: types.SetType{ElemType: types.StringType},
			MarkdownDescription: "Exact explicit column privileges: a map from `SELECT` or `UPDATE` to the set of column names that hold it. " +
				"Views accept only `SELECT`. Column names are quoted; write them as the catalog stores them, which is lower case unless `enable_case_sensitive_identifier` is on. An empty map revokes every column privilege of this tuple; updated in place.",
			Validators: []validator.Map{
				mapvalidator.KeysAre(stringvalidator.OneOf(privilegeNames(columnGrantPrivileges)...)),
				mapvalidator.ValueSetsAre(setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))),
			},
		},
	}
	attributes["grantee"] = privilegeString("Receiving identity name; use `public` for `PUBLIC`. Changing it replaces the grant.", false)
	attributes["grantee_type"] = privilegeString("`ROLE`, `USER`, `GROUP`, or `PUBLIC`. Changing it replaces the grant.", false, "ROLE", "USER", "GROUP", "PUBLIC")
	resp.Schema = schema.Schema{
		MarkdownDescription: "Owns the exact column-level `SELECT` and `UPDATE` privileges of one grantee on one table or view. Table-level grants, other grantees, and other relations are independent.",
		Attributes:          attributes,
	}
}

// read refreshes the tuple's column privileges, reporting false when the grantee, database, or relation is gone.
// managed reads reject privilege names the resource cannot reconcile; lookups observe them instead.
func (r *columnGrantResource) read(ctx context.Context, data *columnGrantModel, managed bool) (columnGrantTarget, bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return columnGrantTarget{}, false, err
	}
	target, err := prepareColumnGrant(*data)
	if err != nil {
		return target, false, err
	}
	for _, check := range target.checks {
		rows, err := r.queryDatabase(ctx, r.database.ValueString(), check.sql, check.parameters)
		if err != nil || len(rows) == 0 {
			return target, false, err
		}
	}
	rows, err := r.selectRows(ctx, target.database, target.query)
	if err != nil {
		return target, false, err
	}
	columns := columnGrantRows(rows)
	if managed {
		for privilege := range columns {
			if !privilegeAllowed(columnGrantPrivileges, privilege) {
				return target, false, fmt.Errorf("unsupported catalog column privilege %q", privilege)
			}
		}
	}
	data.Privileges = columnGrantValue(columns)
	return target, true, nil
}

// reconcile applies the column differences and verifies that the catalog converged.
func (r *columnGrantResource) reconcile(ctx context.Context, data columnGrantModel) error {
	if err := validateColumnGrant(data); err != nil {
		return err
	}
	actual := data
	target, found, err := r.read(ctx, &actual, true)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target relation or grantee does not exist")
	}
	desired := columnGrantDesired(data.Privileges)
	statements, err := columnGrantStatements(target, columnGrantDesired(actual.Privileges), desired)
	if err != nil {
		return err
	}
	if err := r.exec(ctx, target.database, statements...); err != nil {
		return err
	}
	_, found, err = r.read(ctx, &actual, true)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("the relation, database, or grantee disappeared while the column privileges were applied")
	}
	if observed := columnGrantDesired(actual.Privileges); !actual.Privileges.Equal(columnGrantValue(desired)) {
		return columnGrantDivergence(desired, observed)
	}
	return nil
}

// columnGrantDivergence reports a mismatch after apply with both column sets, because the same symptom has several
// causes the user must tell apart.
func columnGrantDivergence(desired, observed columnGrantColumns) error {
	return fmt.Errorf("column privileges did not converge (desired %v, observed %v); possible causes: Redshift folded "+
		"an upper-case column name to lower case (enable_case_sensitive_identifier is off), so write column names as "+
		"the catalog stores them; or the grantee holds the same privilege on the whole table, which supersedes column "+
		"grants; or another session changed the grants concurrently", desired, observed)
}

// Create records tuple ownership before applying idempotent column grants.
func (r *columnGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data columnGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid tuples must fail before ownership is recorded, or Read and Delete could never succeed.
	if err := validateColumnGrant(data); err != nil {
		resp.Diagnostics.AddError("Create column grant", err.Error())
		return
	}
	fields := map[string]string{}
	for _, field := range columnGrantFields {
		fields[field] = columnGrantField(data, field)
	}
	data.ID = r.identity(r.database.ValueString(), fields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Create column grant", err.Error())
	}
}

// columnGrantField returns one identity attribute by its JSON key.
func columnGrantField(data columnGrantModel, field string) string {
	return map[string]types.String{
		"database_name": data.DatabaseName, "schema_name": data.SchemaName, "object_name": data.ObjectName,
		"grantee": data.Grantee, "grantee_type": data.GranteeType,
	}[field].ValueString()
}

// ValidateConfig reports invalid tuples and privileges during planning once the configuration is known.
func (r *columnGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data columnGrantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := validateColumnGrant(data); err != nil {
		resp.Diagnostics.AddError("Invalid column grant", err.Error())
	}
}

// Read refreshes column privileges or removes a tuple whose relation or grantee is gone.
func (r *columnGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data columnGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data, true)
	if err != nil {
		resp.Diagnostics.AddError("Read column grant", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update reconciles the column privileges within the existing tuple.
func (r *columnGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data columnGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Update column grant", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete revokes the tuple's column privileges and verifies that none remain.
func (r *columnGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data columnGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data, true)
	if err != nil {
		resp.Diagnostics.AddError("Read column grant", err.Error())
		return
	}
	if !found {
		return
	}
	data.Privileges = columnGrantValue(nil)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Delete column grant", err.Error())
	}
}

// ImportState restores the tuple from its JSON identity; Read then fills the privileges.
func (r *columnGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, columnGrantFields...)
}
