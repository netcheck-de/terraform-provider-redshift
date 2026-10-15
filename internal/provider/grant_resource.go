package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

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
)

// grantResource owns an exact role, user, or datashare privilege set for a database or schema scope.
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
	// User receives the explicit permissions and may hold them with grant option.
	User types.String `tfsdk:"user"`
	// Datashare receives schema-wide producer permissions instead of a role.
	Datashare types.String `tfsdk:"datashare"`
	// Scope selects the ON DATABASE or ON SCHEMA object, or a FOR … IN object class.
	Scope types.String `tfsdk:"scope"`
	// Privileges is the authoritative explicit privilege set for this tuple.
	Privileges types.Set `tfsdk:"privileges"`
	// GrantOptionPrivileges is the subset a user grantee holds WITH GRANT OPTION.
	GrantOptionPrivileges types.Set `tfsdk:"grant_option_privileges"`
}

var _ = registerResource(newGrantResource)

// newGrantResource constructs an authoritative scoped grant handler.
func newGrantResource() resource.Resource { return &grantResource{} }

// grantRecipientAttributes adds the shared grantee and grantee_type attributes, keeping their validators, with
// descriptions that carry the replacement note of permission tuples.
func grantRecipientAttributes(attributes map[string]schema.Attribute) {
	granteeAttributes(attributes)
	attributes["grantee"] = privilegeString("Receiving identity name; use `public` with `grantee_type = \"PUBLIC\"`. Changing it replaces the grant.", false)
	attributes["grantee_type"] = privilegeString("`ROLE`, `USER`, `GROUP`, or `PUBLIC`. Changing it replaces the grant.", false, "ROLE", "USER", "GROUP", "PUBLIC")
}

// grantPrivilegesAttribute defines the authoritative privilege set with a type-specific description.
func grantPrivilegesAttribute(description string) schema.SetAttribute {
	return schema.SetAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: description}
}

// Metadata identifies the scoped grant resource to Terraform.
func (r *grantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

// Schema defines the recipient/database/scope tuple and desired privilege set.
func (r *grantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	recipients := path.Expressions{path.MatchRoot("role"), path.MatchRoot("user"), path.MatchRoot("datashare")}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Owns the exact privilege set for one role, user, or producer datashare, database, and scope. Other scopes and grantees are independent. `grant_option_privileges` owns which of them a user grantee holds `WITH GRANT OPTION`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database_name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local or shared database receiving scoped grants. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"schema_name": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Required for `SCHEMA`; optional for `TABLES`, `FUNCTIONS`, `PROCEDURES`, and `TEMPLATES` to limit the grant to one schema. Omit for `DATABASE`, `SCHEMAS`, `LANGUAGES`, and `COPY JOBS`. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Receiving Redshift role; configure exactly one of `role`, `user`, or `datashare`. Changing it replaces the grant.",
				Validators:    []validator.String{stringvalidator.ExactlyOneOf(recipients...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"user": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Receiving database user; the only recipient that can hold grant options. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"datashare": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Producer datashare receiving `SCHEMA` `USAGE` or schema-scoped `TABLES` `SELECT`; requires a local database and `schema_name`. Conflicts with datashare membership resources for the same tuple. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scope": schema.StringAttribute{
				Required: true, MarkdownDescription: "`DATABASE` or `SCHEMA` for the database or schema itself, or the object class of a scoped grant that covers current and future objects: `SCHEMAS`, `TABLES`, `FUNCTIONS`, `PROCEDURES`, `LANGUAGES`, `COPY JOBS`, or `TEMPLATES`. `FUNCTIONS` and `PROCEDURES` share one Redshift catalog scope. Changing it replaces the grant.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf(grantScopes...)},
			},
			"privileges": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				MarkdownDescription: "Desired uppercase SQL privileges; updated in place. An empty set revokes all grants for this tuple. `DATABASE`: `CREATE`, `USAGE`, `TEMPORARY`, `ALTER`. `SCHEMAS` and `SCHEMA`: `CREATE`, `USAGE`, `ALTER`, `DROP`. `TABLES`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`, `REFERENCES`. `FUNCTIONS` and `PROCEDURES`: `EXECUTE`. `LANGUAGES`: `USAGE`. `COPY JOBS`: `CREATE`, `ALTER`, `DROP`. `TEMPLATES`: `ALTER`, `DROP`, `USAGE`.",
				Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(grantScopeNames()...))},
			},
			"grant_option_privileges": grantOptionAttribute(),
		},
	}
}

// validate checks scope, recipient, privilege, and grant option rules without issuing SQL.
func (data grantModel) validate() error {
	schemaName, scope := knownString(data.SchemaName), data.Scope.ValueString()
	if _, ok := grantScopePrivileges[scope]; !ok {
		return fmt.Errorf("unsupported scope %q", scope)
	}
	if (scope == "SCHEMA" && schemaName == "") || (schemaName != "" && !slices.Contains(grantSchemaScopes, scope)) {
		return fmt.Errorf("schema_name is required for SCHEMA and only valid with SCHEMA, TABLES, FUNCTIONS, PROCEDURES, or TEMPLATES scope")
	}
	kind, _, err := data.recipient()
	if err != nil {
		return err
	}
	if kind == "DATASHARE" && (schemaName == "" || (scope != "SCHEMA" && scope != "TABLES")) {
		return fmt.Errorf("datashare grants require a local database, schema_name, SCHEMA or TABLES scope, and no role")
	}
	allowed := data.allowed()
	privileges := knownStrings(data.Privileges)
	for _, privilege := range privileges {
		if !privilegeAllowed(allowed, privilege) {
			if kind == "DATASHARE" {
				return fmt.Errorf("datashare %s grants support only %s", scope, allowed[0])
			}
			return fmt.Errorf("%s grants support only %s, not %q", scope, strings.Join(privilegeNames(allowed), ", "), privilege)
		}
	}
	options := knownStrings(data.GrantOptionPrivileges)
	if len(options) != 0 && kind != "USER" {
		return fmt.Errorf("grant_option_privileges requires a user recipient; Redshift grants options only to users")
	}
	for _, option := range options {
		if !slices.Contains(privileges, option) {
			return fmt.Errorf("grant option privilege %q is not in privileges", option)
		}
	}
	return nil
}

// read refreshes explicit scoped privileges and grant options, routing local grants to their database and shared
// grants to admin. It returns the database that grants for this tuple run in.
func (r *grantResource) read(ctx context.Context, data *grantModel) (string, bool, error) {
	schemaName := knownString(data.SchemaName)
	if err := data.validateTuple(); err != nil {
		return "", false, err
	}
	admin := r.database.ValueString()
	if err := r.bound(data.ID, admin); err != nil {
		return admin, false, err
	}
	rows, err := r.selectRows(ctx, admin, grantDatabaseTypeQuery(data.DatabaseName.ValueString()))
	if err != nil || len(rows) == 0 {
		return admin, false, err
	}
	target := admin
	switch rows[0]["database_type"] {
	case "local":
		target = data.DatabaseName.ValueString()
	case "shared":
	default:
		return target, false, fmt.Errorf("scoped grants require a local or shared database")
	}
	kind, identity, _ := data.recipient()
	parentDatabase := admin
	if kind == "DATASHARE" {
		if rows[0]["database_type"] != "local" {
			return target, false, fmt.Errorf("datashare grants require a local database, schema_name, SCHEMA or TABLES scope, and no role")
		}
		identity, parentDatabase = "ds:"+identity, target
	}
	parents, err := r.selectRows(ctx, parentDatabase, grantRecipientQuery(*data))
	if err != nil || len(parents) == 0 {
		return target, false, err
	}
	if schemaName != "" && rows[0]["database_type"] == "local" {
		// A schema dropped outside Terraform removes the grant instead of failing every refresh.
		schemas, err := r.selectRows(ctx, admin, privilegeSchemaQuery(data.DatabaseName.ValueString(), schemaName))
		if err != nil || len(schemas) == 0 {
			return target, false, err
		}
	}
	rows, err = r.queryDatabase(ctx, target, readGrantStatement(*data), nil)
	if err != nil {
		return target, false, err
	}
	privileges, options := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if !data.grantRowMatches(row, identity) {
			continue
		}
		name := normalizePrivilege(row["privilege_type"])
		privileges[name] = true
		// Another grantor's plain grant can report the same privilege, so one row with the option is enough.
		options[name] = options[name] || row["admin_option"] == "true" || row["admin_option"] == "t"
	}
	data.Privileges, data.GrantOptionPrivileges = grantSet(privileges, nil), grantSet(privileges, options)
	return target, true, nil
}

// grantSet returns the sorted names of values, limited to those selected when selected is not nil.
func grantSet(values, selected map[string]bool) types.Set {
	var names []string
	for name := range values {
		if selected == nil || selected[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	elements := make([]attr.Value, 0, len(names))
	for _, name := range names {
		elements = append(elements, types.StringValue(name))
	}
	return types.SetValueMust(types.StringType, elements)
}

// validateTuple checks the identity attributes only, so refreshes of a valid tuple ignore the desired sets.
func (data grantModel) validateTuple() error {
	tuple := data
	tuple.Privileges, tuple.GrantOptionPrivileges = types.SetNull(types.StringType), types.SetNull(types.StringType)
	return tuple.validate()
}

// reconcile revokes unexpected privileges and options, adds missing ones, and verifies the exact sets.
func (r *grantResource) reconcile(ctx context.Context, data grantModel) error {
	if err := data.validate(); err != nil {
		return err
	}
	actual := data
	target, found, err := r.read(ctx, &actual)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target database or receiving identity does not exist")
	}
	allowed := data.allowed()
	current := knownStrings(actual.Privileges)
	for _, name := range current {
		if !privilegeAllowed(allowed, name) {
			return fmt.Errorf("unsupported catalog privilege %q", name)
		}
	}
	spec, err := data.spec()
	if err != nil {
		return err
	}
	desiredOptions := knownStrings(data.GrantOptionPrivileges)
	// Removals run before additions; each operation is retryable.
	statements, err := privilegeOptionStatements(spec, allowed,
		privilegeSets{privileges: current, options: knownStrings(actual.GrantOptionPrivileges)},
		privilegeSets{privileges: knownStrings(data.Privileges), options: desiredOptions})
	if err != nil {
		return err
	}
	if err := r.exec(ctx, target, statements...); err != nil {
		return err
	}
	_, found, err = r.read(ctx, &actual)
	if err == nil && (!found || !slices.Equal(knownStrings(actual.Privileges), knownStrings(data.Privileges)) || !slices.Equal(knownStrings(actual.GrantOptionPrivileges), desiredOptions)) {
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
	kind, name, _ := data.recipient()
	// Grants are idempotent; retain ownership even after a partially applied set.
	fields := map[string]string{"database_name": data.DatabaseName.ValueString(), "scope": data.Scope.ValueString(), grantRecipientField[kind]: name}
	if schemaName := knownString(data.SchemaName); schemaName != "" {
		fields["schema_name"] = schemaName
	}
	data.ID = r.identity(r.database.ValueString(), fields)
	data.GrantOptionPrivileges = types.SetValueMust(types.StringType, grantStringValues(knownStrings(data.GrantOptionPrivileges)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Create scoped grant", err.Error())
	}
}

// grantRecipientField maps a recipient kind to its attribute and identity key.
var grantRecipientField = map[string]string{"ROLE": "role", "USER": "user", "DATASHARE": "datashare"}

// grantStringValues converts names to framework values.
func grantStringValues(names []string) []attr.Value {
	values := make([]attr.Value, 0, len(names))
	for _, name := range names {
		values = append(values, types.StringValue(name))
	}
	return values
}

// ValidateConfig reports invalid scope, recipient, privilege, and grant option combinations during planning.
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
	data.GrantOptionPrivileges = types.SetValueMust(types.StringType, grantStringValues(knownStrings(data.GrantOptionPrivileges)))
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
	data.Privileges, data.GrantOptionPrivileges = types.SetValueMust(types.StringType, nil), types.SetValueMust(types.StringType, nil)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Delete scoped grant", err.Error())
	}
}

// ImportState restores a scoped grant from the JSON identity Create records: one recipient key and an optional
// schema binding.
func (r *grantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var values map[string]string
	recipient := "role"
	if json.Unmarshal([]byte(req.ID), &values) == nil {
		for _, field := range []string{"user", "datashare"} {
			if values[field] != "" {
				recipient = field
			}
		}
	}
	importIdentity(ctx, req, resp, "database_name", recipient, "scope")
	if resp.Diagnostics.HasError() {
		return
	}
	if values["schema_name"] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("schema_name"), values["schema_name"])...)
	}
}
