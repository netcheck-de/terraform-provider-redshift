package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

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

// privilegeResource shares exact-set reconciliation, while each grant supplies its SQL/catalog contract.
type privilegeResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
	// name is the Terraform resource suffix for this concrete permission contract.
	name string
	// attributes defines this contract's identity fields and privilege set.
	attributes map[string]schema.Attribute
	// fields lists identity attributes persisted in JSON import IDs.
	fields []string
	// prepare validates the tuple and supplies its catalog and mutation contract.
	prepare func(types.Object) (privilegeTarget, error)
}

// catalogCheck is a parameterized statement used for parent existence or permission reads.
type catalogCheck struct {
	// sql is a trusted catalog statement containing named parameter placeholders.
	sql string
	// parameters binds values without interpolating them into catalog SQL.
	parameters map[string]string
}

// privilegeTarget describes catalog discovery and SQL mutation for one authoritative permission tuple.
type privilegeTarget struct {
	// database overrides the admin connection for database-local permission operations.
	database string
	// checks confirms parent objects and identities still exist.
	checks []catalogCheck
	// query reads the explicit privilege set owned by this tuple.
	query catalogCheck
	// prefix precedes GRANT/REVOKE, for example ALTER DEFAULT PRIVILEGES.
	prefix string
	// object follows the privilege keyword, including its ON clause when needed.
	object string
	// recipient is a quoted SQL grantee with any required ROLE/GROUP prefix.
	recipient string
	// suffix contains any SQL following the grantee.
	suffix string
	// allowed is the privilege allowlist used before emitting mutation SQL.
	allowed []string
	// filter optionally excludes rows belonging to other grantees or scopes.
	filter func(sqlclient.Row) bool
	// statement overrides standard GRANT/REVOKE formatting for ASSUMEROLE command grants.
	statement func(string, string) string
}

// privilegeString defines an immutable nonempty permission identity field.
func privilegeString(description string, optional bool, choices ...string) schema.StringAttribute {
	validators := []validator.String{stringvalidator.LengthAtLeast(1)}
	if len(choices) != 0 {
		validators = append(validators, stringvalidator.OneOf(choices...))
	}
	return schema.StringAttribute{
		Required: !optional, Optional: optional, MarkdownDescription: description,
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Validators: validators,
	}
}

// objectString retrieves a string field, treating absent or null fields as empty.
func objectString(data types.Object, key string) string {
	value, ok := data.Attributes()[key]
	if !ok {
		return ""
	}
	return value.(types.String).ValueString()
}

// withPrivileges copies an object with a deterministic Terraform privilege set.
func withPrivileges(data types.Object, privileges []string) types.Object {
	sort.Strings(privileges)
	values := make([]attr.Value, 0, len(privileges))
	for _, privilege := range privileges {
		values = append(values, types.StringValue(privilege))
	}
	attributes := data.Attributes()
	attributes["privileges"] = types.SetValueMust(types.StringType, values)
	return types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes)
}

// principal formats a grantee and supplies its catalog existence check.
func principal(data types.Object) (string, catalogCheck, error) {
	name, kind := objectString(data, "grantee"), objectString(data, "grantee_type")
	switch kind {
	case "ROLE":
		return "ROLE " + sqlclient.Identifier(name), catalogCheck{"SELECT role_name FROM svv_roles WHERE role_name = :name", map[string]string{"name": name}}, nil
	case "USER":
		return sqlclient.Identifier(name), catalogCheck{"SELECT usename FROM pg_user WHERE usename = :name", map[string]string{"name": name}}, nil
	case "GROUP":
		return "GROUP " + sqlclient.Identifier(name), catalogCheck{"SELECT groname FROM pg_group WHERE groname = :name", map[string]string{"name": name}}, nil
	case "PUBLIC":
		if name != "public" {
			return "", catalogCheck{}, fmt.Errorf("PUBLIC requires grantee = public")
		}
		return "PUBLIC", catalogCheck{}, nil
	default:
		return "", catalogCheck{}, fmt.Errorf("unsupported grantee_type %q", kind)
	}
}

// Metadata identifies the concrete permission resource selected by its SQL contract.
func (r *privilegeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.name
}

// Schema exposes this grant's identity fields and authoritative privilege set.
func (r *privilegeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Owns the exact explicit privilege set for one permission tuple. Empty privileges revoke the owned grants.", Attributes: r.attributes}
}

// read verifies parents, normalizes explicit privileges, and rejects unmanaged grant options.
func (r *privilegeResource) read(ctx context.Context, data *types.Object) (privilegeTarget, bool, error) {
	return r.readPrivileges(ctx, data, true)
}

// readPrivileges normalizes catalog permissions; managed reads additionally reject unsafe mutation cases.
func (r *privilegeResource) readPrivileges(ctx context.Context, data *types.Object, managed bool) (privilegeTarget, bool, error) {
	if err := r.bound(data.Attributes()["id"].(types.String), r.database.ValueString()); err != nil {
		return privilegeTarget{}, false, err
	}
	target, err := r.prepare(*data)
	if err != nil {
		return target, false, err
	}
	connection, err := r.connection(r.database.ValueString())
	if err != nil {
		return target, false, err
	}
	for _, check := range target.checks {
		if check.sql == "" {
			continue
		}
		rows, err := r.client.Query(ctx, connection, check.sql, check.parameters)
		if err != nil || len(rows) == 0 {
			return target, false, err
		}
	}
	if target.database != "" {
		connection.Database = target.database
	}
	rows, err := r.client.Query(ctx, connection, target.query.sql, target.query.parameters)
	if err != nil {
		return target, false, err
	}
	privileges := map[string]bool{}
	for _, row := range rows {
		if target.filter != nil && !target.filter(row) {
			continue
		}
		if managed && (row["admin_option"] == "true" || row["admin_option"] == "t") {
			return target, false, fmt.Errorf("grant options are not managed; remove them explicitly before importing")
		}
		name := normalizePrivilege(row["privilege_type"])
		if managed && !slices.Contains(target.allowed, name) {
			return target, false, fmt.Errorf("unsupported catalog privilege %q", name)
		}
		privileges[name] = true
	}
	values := make([]string, 0, len(privileges))
	for privilege := range privileges {
		values = append(values, privilege)
	}
	*data = withPrivileges(*data, values)
	return target, true, nil
}

// normalizePrivilege maps catalog privilege abbreviations to the names accepted in configuration.
func normalizePrivilege(name string) string {
	switch name {
	case "EXFUNC":
		return "EXTERNAL FUNCTION"
	case "TEMP":
		return "TEMPORARY"
	default:
		return name
	}
}

// validate checks the tuple and desired privileges without issuing SQL.
func (r *privilegeResource) validate(data types.Object) error {
	target, err := r.prepare(data)
	if err != nil {
		return err
	}
	for _, value := range data.Attributes()["privileges"].(types.Set).Elements() {
		if !slices.Contains(target.allowed, value.(types.String).ValueString()) {
			return fmt.Errorf("unsupported privilege %q", value.(types.String).ValueString())
		}
	}
	return nil
}

// reconcile applies privilege-set differences and verifies catalog convergence.
func (r *privilegeResource) reconcile(ctx context.Context, data types.Object) error {
	if err := r.validate(data); err != nil {
		return err
	}
	actual := data
	target, found, err := r.read(ctx, &actual)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target object or identity does not exist")
	}
	desired := data.Attributes()["privileges"].(types.Set)
	current := actual.Attributes()["privileges"].(types.Set)
	connection, _ := r.connection(r.database.ValueString())
	if target.database != "" {
		connection.Database = target.database
	}
	for _, operation := range []struct {
		verb, direction string
		from, to        types.Set
	}{{"REVOKE", "FROM", current, desired}, {"GRANT", "TO", desired, current}} {
		for _, value := range operation.from.Elements() {
			if slices.ContainsFunc(operation.to.Elements(), func(other attr.Value) bool { return other.Equal(value) }) {
				continue
			}
			sql := strings.TrimSpace(target.prefix + operation.verb + " " + value.(types.String).ValueString() + target.object + " " + operation.direction + " " + target.recipient + target.suffix)
			if target.statement != nil {
				sql = target.statement(operation.verb, value.(types.String).ValueString())
			}
			if _, err := r.client.Query(ctx, connection, sql, nil); err != nil {
				return err
			}
		}
	}
	_, found, err = r.read(ctx, &actual)
	if err == nil && (!found || !actual.Attributes()["privileges"].Equal(desired)) {
		return fmt.Errorf("privileges did not converge")
	}
	return err
}

// Create records tuple ownership before applying idempotent privilege mutations.
func (r *privilegeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid tuples must fail before ownership is recorded, or Read and Delete could never succeed.
	if err := r.validate(data); err != nil {
		resp.Diagnostics.AddError("Create "+r.name, err.Error())
		return
	}
	fields := map[string]string{}
	for _, field := range r.fields {
		if value := objectString(data, field); value != "" {
			fields[field] = value
		}
	}
	attributes := data.Attributes()
	attributes["id"] = r.identity(r.database.ValueString(), fields)
	data = types.ObjectValueMust(data.AttributeTypes(ctx), attributes)
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Create "+r.name, err.Error())
	}
}

// ValidateConfig reports invalid tuples and privileges during planning once the configuration is known.
func (r *privilegeResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := r.validate(data); err != nil {
		resp.Diagnostics.AddError("Invalid "+r.name, err.Error())
	}
}

// Read refreshes explicit privileges or removes a tuple whose parents are missing.
func (r *privilegeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read "+r.name, err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

// Update reconciles privileges within the existing permission identity.
func (r *privilegeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data); err != nil {
		resp.Diagnostics.AddError("Update "+r.name, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

// Delete reconciles the owned privilege set to empty, leaving other tuples intact.
func (r *privilegeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read "+r.name, err.Error())
		return
	}
	if !found {
		return
	}
	if err := r.reconcile(ctx, withPrivileges(data, nil)); err != nil {
		resp.Diagnostics.AddError("Delete "+r.name, err.Error())
	}
}

// ImportState restores required and optional permission identity fields from JSON.
func (r *privilegeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	var required []string
	for _, field := range r.fields {
		if !r.attributes[field].IsOptional() {
			required = append(required, field)
		}
	}
	importIdentity(ctx, req, resp, required...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, field := range r.fields {
		if r.attributes[field].IsOptional() && values[field] != "" {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(field), values[field])...)
		}
	}
}

// privilegeAttributes supplies the shared ID and authoritative privilege-set attributes.
func privilegeAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":         idAttribute(),
		"privileges": schema.SetAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Exact explicit privilege set. An empty set revokes owned privileges."},
	}
}

// granteeAttributes adds explicit SQL identity name and type attributes.
func granteeAttributes(attributes map[string]schema.Attribute) {
	attributes["grantee"] = privilegeString("Receiving identity name; use public for PUBLIC.", false)
	attributes["grantee_type"] = privilegeString("ROLE, USER, GROUP, or PUBLIC.", false, "ROLE", "USER", "GROUP", "PUBLIC")
}
