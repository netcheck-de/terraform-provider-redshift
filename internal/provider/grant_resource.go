package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// scopedPrivileges limits emitted SQL to supported privilege keywords.
var scopedPrivileges = []string{"USAGE", "CREATE", "TEMPORARY", "SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "TRUNCATE", "ALTER", "EXECUTE"}

// grantResource owns an exact role or datashare privilege set for a database or schema scope.
type grantResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// grantModel is the Terraform state for one recipient/database/scope permission tuple.
type grantModel struct {
	// ID records the administration binding and permission tuple.
	ID types.String `tfsdk:"id"`
	// DatabaseName receives the scoped permissions.
	DatabaseName types.String `tfsdk:"database_name"`
	// SchemaName optionally narrows the scope to one schema.
	SchemaName types.String `tfsdk:"schema_name"`
	// Role receives the explicit permissions.
	Role types.String `tfsdk:"role"`
	// Datashare receives schema-wide producer permissions instead of a role.
	Datashare types.String `tfsdk:"datashare"`
	// Scope selects DATABASE, SCHEMAS, SCHEMA, TABLES, FUNCTIONS, or PROCEDURES.
	Scope types.String `tfsdk:"scope"`
	// Privileges is the authoritative explicit privilege set for this tuple.
	Privileges types.Set `tfsdk:"privileges"`
}

// newGrantResource constructs an authoritative scoped grant handler.
func newGrantResource() resource.Resource { return &grantResource{} }

// Metadata identifies the scoped grant resource to Terraform.
func (r *grantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

// Schema defines the role/database/scope tuple and desired privilege set.
func (r *grantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Owns the exact privilege set for one role or producer datashare, database, and scope. Other scopes and grantees are independent.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database_name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local or shared database receiving scoped grants; changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"schema_name": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Required for `SCHEMA`; optional for `TABLES`, `FUNCTIONS`, and `PROCEDURES` to limit the grant to one schema. Omit for `DATABASE` and `SCHEMAS`. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Receiving Redshift role; configure exactly one of `role` or `datashare`. Changing it replaces the grant.",
				Validators:    []validator.String{stringvalidator.ExactlyOneOf(path.MatchRoot("role"), path.MatchRoot("datashare"))},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"datashare": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Producer datashare receiving `SCHEMA` `USAGE` or schema-scoped `TABLES` `SELECT`; requires a local database and `schema_name`. Conflicts with `role` and with datashare membership resources for the same tuple. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scope": schema.StringAttribute{
				Required: true, MarkdownDescription: "`DATABASE`, `SCHEMAS`, `SCHEMA`, `TABLES`, `FUNCTIONS`, or `PROCEDURES`. `FUNCTIONS` and `PROCEDURES` share one Redshift catalog scope. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("DATABASE", "SCHEMAS", "SCHEMA", "TABLES", "FUNCTIONS", "PROCEDURES")},
			},
			"privileges": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				MarkdownDescription: "Desired uppercase SQL privileges; updated in place. An empty set revokes all grants for this tuple.",
				Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(scopedPrivileges...))},
			},
		},
	}
}

// validate checks scope, recipient, and datashare privilege rules without issuing SQL.
func (data grantModel) validate() error {
	schemaName, scope := data.SchemaName.ValueString(), data.Scope.ValueString()
	if (scope == "SCHEMA" && schemaName == "") || (schemaName != "" && scope != "SCHEMA" && scope != "TABLES" && scope != "FUNCTIONS" && scope != "PROCEDURES") {
		return fmt.Errorf("schema_name is required for SCHEMA and only valid with SCHEMA, TABLES, FUNCTIONS, or PROCEDURES scope")
	}
	if data.Datashare.ValueString() == "" {
		if data.Role.ValueString() == "" {
			return fmt.Errorf("configure exactly one nonempty role or datashare")
		}
		return nil
	}
	if data.Role.ValueString() != "" || schemaName == "" || (scope != "SCHEMA" && scope != "TABLES") {
		return fmt.Errorf("datashare grants require a local database, schema_name, SCHEMA or TABLES scope, and no role")
	}
	privilege := "USAGE"
	if scope == "TABLES" {
		privilege = "SELECT"
	}
	for _, value := range data.Privileges.Elements() {
		if value.(types.String).ValueString() != privilege {
			return fmt.Errorf("datashare %s grants support only %s", scope, privilege)
		}
	}
	return nil
}

// read refreshes explicit scoped privileges, routing local grants to their database and shared grants to admin.
func (r *grantResource) read(ctx context.Context, data *grantModel) (sqlclient.Connection, bool, error) {
	schemaName, scope := data.SchemaName.ValueString(), data.Scope.ValueString()
	if err := data.validate(); err != nil {
		return sqlclient.Connection{}, false, err
	}
	target, err := r.connection(r.database.ValueString())
	if err != nil {
		return target, false, err
	}
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return target, false, err
	}
	rows, err := r.query(ctx, "SELECT database_type FROM svv_redshift_databases WHERE database_name = :database", map[string]string{"database": data.DatabaseName.ValueString()})
	if err != nil || len(rows) == 0 {
		return target, false, err
	}
	switch rows[0]["database_type"] {
	case "local":
		target.Database = data.DatabaseName.ValueString()
	case "shared":
	default:
		return target, false, fmt.Errorf("scoped grants require a local or shared database")
	}
	identity := data.Role.ValueString()
	query := "SHOW GRANTS FOR ROLE " + sqlclient.Identifier(identity) + " FROM DATABASE " + sqlclient.Identifier(data.DatabaseName.ValueString())
	parentQuery := "SELECT role_name FROM svv_roles WHERE role_name = :role"
	parameters := map[string]string{"role": identity}
	if data.Datashare.ValueString() != "" {
		if rows[0]["database_type"] != "local" {
			return target, false, fmt.Errorf("datashare grants require a local database, schema_name, SCHEMA or TABLES scope, and no role")
		}
		identity = "ds:" + data.Datashare.ValueString()
		query = "SHOW GRANTS ON SCHEMA " + sqlclient.Identifier(schemaName)
		parentQuery = "SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share"
		parameters = map[string]string{"share": data.Datashare.ValueString()}
	}
	parentTarget := target
	if data.Datashare.ValueString() == "" {
		parentTarget.Database = r.database.ValueString()
	}
	roles, err := r.client.Query(ctx, parentTarget, parentQuery, parameters)
	if err != nil || len(roles) == 0 {
		return target, false, err
	}
	if schemaName != "" && rows[0]["database_type"] == "local" {
		// A schema dropped outside Terraform removes the grant instead of failing every refresh.
		schemas, err := r.query(ctx, "SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema", map[string]string{"database": data.DatabaseName.ValueString(), "schema": schemaName})
		if err != nil || len(schemas) == 0 {
			return target, false, err
		}
	}
	rows, err = r.client.Query(ctx, target, query, nil)
	if err != nil {
		return target, false, err
	}
	privileges := make(map[string]bool)
	for _, row := range rows {
		objectType := "DATABASE"
		if schemaName != "" {
			objectType = "SCHEMA"
		}
		matchingScope := row["privilege_scope"] == scope || ((scope == "FUNCTIONS" || scope == "PROCEDURES") && (row["privilege_scope"] == "FUNCTIONS" || row["privilege_scope"] == "PROCEDURES"))
		if row["identity_name"] == identity && row["database_name"] == data.DatabaseName.ValueString() && row["object_type"] == objectType && matchingScope && (schemaName == "" || row["schema_name"] == schemaName) {
			privileges[normalizePrivilege(row["privilege_type"])] = true
		}
	}
	values := make([]string, 0, len(privileges))
	for privilege := range privileges {
		values = append(values, privilege)
	}
	sort.Strings(values)
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	data.Privileges = types.SetValueMust(types.StringType, elements)
	return target, true, nil
}

// reconcile revokes unexpected privileges, adds missing ones, and verifies the exact set.
func (r *grantResource) reconcile(ctx context.Context, data grantModel) error {
	actual := data
	target, found, err := r.read(ctx, &actual)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target database or receiving role does not exist")
	}
	object := "ON DATABASE " + sqlclient.Identifier(data.DatabaseName.ValueString())
	if data.SchemaName.ValueString() != "" {
		if data.Scope.ValueString() == "SCHEMA" {
			object = "ON SCHEMA " + sqlclient.Identifier(data.DatabaseName.ValueString()) + "." + sqlclient.Identifier(data.SchemaName.ValueString())
		} else {
			object = "FOR " + data.Scope.ValueString() + " IN SCHEMA " + sqlclient.Identifier(data.SchemaName.ValueString()) + " DATABASE " + sqlclient.Identifier(data.DatabaseName.ValueString())
		}
	} else if data.Scope.ValueString() != "DATABASE" {
		object = "FOR " + data.Scope.ValueString() + " IN DATABASE " + sqlclient.Identifier(data.DatabaseName.ValueString())
	}
	recipient := "ROLE " + sqlclient.Identifier(data.Role.ValueString())
	if data.Datashare.ValueString() != "" {
		recipient = "DATASHARE " + sqlclient.Identifier(data.Datashare.ValueString())
		object = "ON SCHEMA " + sqlclient.Identifier(data.SchemaName.ValueString())
		if data.Scope.ValueString() == "TABLES" {
			object = "FOR TABLES IN SCHEMA " + sqlclient.Identifier(data.SchemaName.ValueString())
		}
	}
	for _, privilege := range actual.Privileges.Elements() {
		if name := privilege.(types.String).ValueString(); !slices.Contains(scopedPrivileges, name) {
			return fmt.Errorf("unsupported catalog privilege %q", name)
		}
	}
	// Revoke extras before adding desired privileges; each operation is retryable.
	for _, privilege := range actual.Privileges.Elements() {
		if slices.ContainsFunc(data.Privileges.Elements(), func(value attr.Value) bool { return value.Equal(privilege) }) {
			continue
		}
		name := privilege.(types.String).ValueString()
		if _, err := r.client.Query(ctx, target, "REVOKE "+name+" "+object+" FROM "+recipient, nil); err != nil {
			return err
		}
	}
	for _, privilege := range data.Privileges.Elements() {
		if slices.ContainsFunc(actual.Privileges.Elements(), func(value attr.Value) bool { return value.Equal(privilege) }) {
			continue
		}
		if _, err := r.client.Query(ctx, target, "GRANT "+privilege.(types.String).ValueString()+" "+object+" TO "+recipient, nil); err != nil {
			return err
		}
	}
	_, found, err = r.read(ctx, &actual)
	if err == nil && (!found || !actual.Privileges.Equal(data.Privileges)) {
		err = fmt.Errorf("scoped privileges did not converge after reconciliation")
	}
	return err
}

// Create retains tuple ownership before reconciling idempotent grants.
func (r *grantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid tuples must fail before ownership is recorded, or Read and Delete could never succeed.
	if err := data.validate(); err != nil {
		resp.Diagnostics.AddError("Create scoped grant", err.Error())
		return
	}
	// Grants are idempotent; retain ownership even after a partially applied set.
	fields := map[string]string{"database_name": data.DatabaseName.ValueString(), "role": data.Role.ValueString(), "scope": data.Scope.ValueString()}
	if data.Datashare.ValueString() != "" {
		delete(fields, "role")
		fields["datashare"] = data.Datashare.ValueString()
	}
	if !data.SchemaName.IsNull() {
		fields["schema_name"] = data.SchemaName.ValueString()
	}
	data.ID = r.identity(r.database.ValueString(), fields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Create scoped grant", err.Error())
	}
}

// ValidateConfig reports invalid scope, recipient, and datashare combinations during planning.
func (r *grantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := data.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid scoped grant", err.Error())
	}
}

// Read refreshes explicit privileges or removes a grant whose parent is absent.
func (r *grantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read scoped grant", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update reconciles the desired privilege set within the existing tuple.
func (r *grantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Update scoped grant", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete revokes privileges owned by this scope without touching other tuples.
func (r *grantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data grantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read scoped grant", err.Error())
		return
	}
	if !found {
		return
	}
	data.Privileges = types.SetValueMust(types.StringType, nil)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Delete scoped grant", err.Error())
	}
}

// ImportState restores a scoped grant, including its optional schema binding.
func (r *grantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var values map[string]string
	fields := []string{"database_name", "role", "scope"}
	if json.Unmarshal([]byte(req.ID), &values) == nil && values["datashare"] != "" {
		fields = []string{"database_name", "datashare", "scope"}
	}
	importIdentity(ctx, req, resp, fields...)
	if err := json.Unmarshal([]byte(req.ID), &values); err == nil && values["schema_name"] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("schema_name"), values["schema_name"])...)
	}
}
