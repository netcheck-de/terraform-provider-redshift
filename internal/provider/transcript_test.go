package provider

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transcriptCase drives one Terraform operation against a fresh fake and records its SQL conversation.
type transcriptCase struct {
	// name becomes the transcript file name within the resource's group.
	name string
	// operation is create, read, update, delete, or import (create unrecorded, then import and read).
	operation string
	// catalog returns the fake in its state before the operation.
	catalog func() sqlclient.Client
	// prior is the state for read, update, and delete.
	prior any
	// planned is the plan for create and update, and the created configuration for import.
	planned any
	// config overrides planned as configuration, supplying write-only values that plans never carry.
	config any
}

// resourceTypeName returns the Terraform type without the provider prefix, which names transcript groups.
func resourceTypeName(factory func() resource.Resource) string {
	var metadata resource.MetadataResponse
	factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	return strings.TrimPrefix(metadata.TypeName, "redshift_")
}

// emptyState returns a null state of the resource's schema, as Terraform sends before creation and import.
func emptyState(t *testing.T, r resource.Resource) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	require.False(t, schema.Diagnostics.HasError(), "%v", schema.Diagnostics)
	return tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(context.Background()), nil)}
}

// applyOperation runs one lifecycle RPC with distinct prior state, plan, and configuration.
func applyOperation(t *testing.T, r resource.Resource, operation string, prior, planned, config any) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	if config == nil {
		config = planned
	}
	switch operation {
	case "create":
		resp := resource.CreateResponse{State: emptyState(t, r)}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, planned)), Config: tfsdk.Config(testState(t, r, config))}, &resp)
		return resp.State, resp.Diagnostics
	case "read":
		state := testState(t, r, prior)
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)
		if resp.State.Raw.IsNull() {
			resp.Diagnostics.AddError("Read removed the resource", "The fake catalog does not contain the object.")
		}
		return resp.State, resp.Diagnostics
	case "update":
		state := testState(t, r, prior)
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan(testState(t, r, planned)), State: state, Config: tfsdk.Config(testState(t, r, config))}, &resp)
		return resp.State, resp.Diagnostics
	case "delete":
		state := testState(t, r, prior)
		resp := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		return resp.State, resp.Diagnostics
	default:
		t.Fatalf("unknown transcript operation %q", operation)
		return tfsdk.State{}, nil
	}
}

// importAndRead imports a JSON identity and refreshes it, as terraform import does.
func importAndRead(t *testing.T, r resource.Resource, id string) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
	if imported.Diagnostics.HasError() {
		return imported.Diagnostics
	}
	resp := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &resp)
	if resp.State.Raw.IsNull() {
		resp.Diagnostics.AddError("Read removed the imported resource", "The fake catalog does not contain the object.")
	}
	return resp.Diagnostics
}

// runTranscripts records each case into group; every case must succeed, so transcripts show converging flows.
func runTranscripts(t *testing.T, group string, factory func() resource.Resource, cases []transcriptCase) {
	t.Helper()
	for _, test := range cases {
		recorder := &recordingClient{client: test.catalog()}
		r := factory()
		configureTestResource(t, r, recorder)
		var diagnostics diag.Diagnostics
		if test.operation == "import" {
			// The identity comes from a real Create, so the transcript pins the ID format Create emits.
			var state tfsdk.State
			state, diagnostics = applyOperation(t, r, "create", nil, test.planned, test.config)
			require.False(t, diagnostics.HasError(), "%s: %v", test.name, diagnostics)
			var id types.String
			require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
			recorder.take()
			diagnostics = importAndRead(t, r, id.ValueString())
		} else {
			_, diagnostics = applyOperation(t, r, test.operation, test.prior, test.planned, test.config)
		}
		require.False(t, diagnostics.HasError(), "%s: %v", test.name, diagnostics)
		checkTranscript(t, group, test.name, recorder.take())
	}
}

// catalogWith returns a fake factory starting from fullCatalog with the given changes.
func catalogWith(changes ...func(*catalog)) func() sqlclient.Client {
	return func() sqlclient.Client {
		c := fullCatalog()
		for _, change := range changes {
			change(c)
		}
		return c
	}
}

// standardTranscripts derives create, read, update, delete, and import flows from a lifecycle case.
func standardTranscripts(test lifecycleCase) []transcriptCase {
	setup := test.applySetup
	absent := func(c *catalog) {
		if test.absent != nil {
			test.absent(c)
		}
	}
	prepared := func(operation string) func(*catalog) {
		return func(c *catalog) { test.applyPrepare(c, operation) }
	}
	cases := []transcriptCase{
		{name: "create", operation: "create", catalog: catalogWith(setup, absent, prepared("create")), planned: test.model},
		{name: "read", operation: "read", catalog: catalogWith(setup, prepared("read")), prior: test.model},
		{name: "update", operation: "update", catalog: catalogWith(setup, prepared("update")), prior: test.model, planned: test.model},
		{name: "delete", operation: "delete", catalog: catalogWith(setup, test.removable, prepared("delete")), prior: test.model},
		{name: "import", operation: "import", catalog: catalogWith(setup, absent, prepared("create")), planned: test.model},
	}
	if field := reflect.ValueOf(test.model).FieldByName("Database"); field.IsValid() {
		// The admin database skips the local-database check, so another database shows that check and routing.
		copied := reflect.New(reflect.TypeOf(test.model)).Elem()
		copied.Set(reflect.ValueOf(test.model))
		copied.FieldByName("Database").Set(reflect.ValueOf(types.StringValue("warehouse")))
		local := func(c *catalog) { c.localDB = true }
		cases = append(cases,
			transcriptCase{name: "create_local_database", operation: "create", catalog: catalogWith(setup, absent, local, prepared("create")), planned: copied.Interface()},
			transcriptCase{name: "read_local_database", operation: "read", catalog: catalogWith(setup, local, prepared("read")), prior: copied.Interface()},
		)
	}
	return cases
}

// scopedGrantCatalog emulates one scoped grant tuple for local, shared, and datashare recipients.
type scopedGrantCatalog struct {
	// databaseType is the svv_redshift_databases type of the granted database.
	databaseType string
	// identity is the SHOW GRANTS identity_name, ds:<share> for datashares.
	identity string
	// schema is the SHOW GRANTS schema_name; empty for database-wide scopes.
	schema string
	// scope is the SHOW GRANTS privilege_scope, which Redshift reports as FUNCTIONS for procedures too.
	scope string
	// privileges records the granted privileges.
	privileges map[string]bool
}

// Query answers existence checks, lists the tuple's grants, and applies GRANT/REVOKE.
func (c *scopedGrantCatalog) Query(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT database_type"):
		return []sqlclient.Row{{"database_type": c.databaseType}}, nil
	case strings.HasPrefix(sql, "SELECT role_name"), strings.HasPrefix(sql, "SELECT share_name"), strings.HasPrefix(sql, "SELECT schema_name"):
		return []sqlclient.Row{{"name": "exists"}}, nil
	case strings.HasPrefix(sql, "SHOW GRANTS"):
		objectType := "DATABASE"
		if c.schema != "" {
			objectType = "SCHEMA"
		}
		rows := []sqlclient.Row{}
		for _, privilege := range slices.Sorted(maps.Keys(c.privileges)) {
			rows = append(rows, sqlclient.Row{"database_name": "analytics", "schema_name": c.schema, "identity_name": c.identity, "object_type": objectType, "privilege_scope": c.scope, "privilege_type": privilege})
		}
		return rows, nil
	case strings.HasPrefix(sql, "GRANT "):
		c.privileges[strings.Fields(sql)[1]] = true
	case strings.HasPrefix(sql, "REVOKE "):
		delete(c.privileges, strings.Fields(sql)[1])
	default:
		return nil, fmt.Errorf("unexpected SQL: %s", sql)
	}
	return nil, nil
}

// grantTranscripts covers every grant scope and recipient form beyond the shared-database lifecycle case.
func grantTranscripts() []transcriptCase {
	var cases []transcriptCase
	for _, shape := range []struct {
		name, databaseType, schema, datashare, scope, catalogScope, privilege string
	}{
		{"database_scope", "local", "", "", "DATABASE", "DATABASE", "CREATE"},
		{"schemas_scope", "local", "", "", "SCHEMAS", "SCHEMAS", "USAGE"},
		{"schema_scope", "local", "serving", "", "SCHEMA", "SCHEMA", "USAGE"},
		{"tables_in_schema", "local", "serving", "", "TABLES", "TABLES", "SELECT"},
		{"tables_in_schema_shared", "shared", "serving", "", "TABLES", "TABLES", "SELECT"},
		{"functions_in_schema", "local", "serving", "", "FUNCTIONS", "FUNCTIONS", "EXECUTE"},
		{"functions_in_database", "local", "", "", "FUNCTIONS", "FUNCTIONS", "EXECUTE"},
		{"procedures_in_schema", "local", "serving", "", "PROCEDURES", "PROCEDURES", "EXECUTE"},
		{"procedures_in_database", "local", "", "", "PROCEDURES", "FUNCTIONS", "EXECUTE"},
		{"datashare_schema", "local", "serving", "producer", "SCHEMA", "SCHEMA", "USAGE"},
		{"datashare_tables", "local", "serving", "producer", "TABLES", "TABLES", "SELECT"},
	} {
		model := grantModel{DatabaseName: types.StringValue("analytics"), Scope: types.StringValue(shape.scope), Role: types.StringValue("example:readers"), User: types.StringNull(), Datashare: types.StringNull(), SchemaName: types.StringNull(), GrantOptionPrivileges: types.SetValueMust(types.StringType, nil)}
		identity := "example:readers"
		if shape.datashare != "" {
			model.Role, model.Datashare, identity = types.StringNull(), types.StringValue(shape.datashare), "ds:"+shape.datashare
		}
		if shape.schema != "" {
			model.SchemaName = types.StringValue(shape.schema)
		}
		model.Privileges = types.SetValueMust(types.StringType, []attr.Value{types.StringValue(shape.privilege)})
		fake := func(granted bool) func() sqlclient.Client {
			return func() sqlclient.Client {
				privileges := map[string]bool{}
				if granted {
					privileges[shape.privilege] = true
				}
				return &scopedGrantCatalog{databaseType: shape.databaseType, identity: identity, schema: shape.schema, scope: shape.catalogScope, privileges: privileges}
			}
		}
		cases = append(cases, transcriptCase{name: "create_" + shape.name, operation: "create", catalog: fake(false), planned: model})
		if shape.datashare != "" {
			// Datashare recipients read SHOW GRANTS ON SCHEMA and import through their own identity fields.
			cases = append(cases,
				transcriptCase{name: "delete_" + shape.name, operation: "delete", catalog: fake(true), prior: model},
				transcriptCase{name: "import_" + shape.name, operation: "import", catalog: fake(false), planned: model},
			)
		}
	}
	return cases
}

// lifecycleVariants adds statement-shape branches that the representative lifecycle models do not reach.
func lifecycleVariants() map[string][]transcriptCase {
	local := databaseModel{Name: types.StringValue("warehouse"), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true)}
	isolated := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(false)}
	public := datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer"), PublicAccessible: types.BoolValue(true)}
	includeNew := datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), IncludeNew: types.BoolValue(true)}
	namespace := datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringNull(), NamespaceID: types.StringValue("12345678-1234-1234-1234-123456789abc")}
	region := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("example_glue"), IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), Region: types.StringValue("eu-central-1"), RefreshRevision: types.StringNull()}
	disabled := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(false)}
	rotated := disabled
	rotated.IAMRoleARN = types.StringValue("role-two")
	userGrant := roleGrantModel{Role: types.StringValue("sys:monitor"), ToRole: types.StringNull(), ToUser: types.StringValue("grafana")}
	userGranted := func(granted bool) func(*catalog) {
		return func(c *catalog) { c.user, c.userGrant = true, granted }
	}
	return map[string][]transcriptCase{
		"database": {
			{name: "create_local", operation: "create", catalog: catalogWith(), planned: local},
			{name: "read_local", operation: "read", catalog: catalogWith(func(c *catalog) { c.localDB = true }), prior: local},
			{name: "delete_local", operation: "delete", catalog: catalogWith(func(c *catalog) { c.localDB, c.share = true, false }), prior: local},
			{name: "import_local", operation: "import", catalog: catalogWith(), planned: local},
			{name: "create_shared_without_permissions", operation: "create", catalog: catalogWith(func(c *catalog) { c.database = false }), planned: isolated},
		},
		"datashare": {
			{name: "create_public", operation: "create", catalog: catalogWith(func(c *catalog) { c.share = false }), planned: public},
			{name: "update_public", operation: "update", catalog: catalogWith(), prior: public, planned: public},
		},
		"share schema": {
			{name: "create_include_new", operation: "create", catalog: catalogWith(func(c *catalog) { c.shareSchema = false }), planned: includeNew},
			{name: "update_include_new", operation: "update", catalog: catalogWith(), prior: includeNew, planned: includeNew},
		},
		"share grant": {
			{name: "create_namespace", operation: "create", catalog: catalogWith(), planned: namespace},
			{name: "read_namespace", operation: "read", catalog: catalogWith(func(c *catalog) { c.shareNamespaceGrant = true }), prior: namespace},
			{name: "delete_namespace", operation: "delete", catalog: catalogWith(func(c *catalog) { c.shareNamespaceGrant = true }), prior: namespace},
			{name: "import_namespace", operation: "import", catalog: catalogWith(), planned: namespace},
		},
		"external schema": {
			{name: "create_region", operation: "create", catalog: catalogWith(func(c *catalog) { c.external = false }), planned: region},
		},
		"identity": {
			{name: "create_disabled", operation: "create", catalog: catalogWith(func(c *catalog) { c.identity = false }), planned: disabled},
			{name: "update_disabled", operation: "update", catalog: catalogWith(), prior: disabled, planned: disabled},
			{name: "update_role_and_disable", operation: "update", catalog: catalogWith(), prior: disabled, planned: rotated},
		},
		"membership": {
			{name: "create_user", operation: "create", catalog: catalogWith(userGranted(false)), planned: userGrant},
			{name: "read_user", operation: "read", catalog: catalogWith(userGranted(true)), prior: userGrant},
			{name: "delete_user", operation: "delete", catalog: catalogWith(userGranted(true)), prior: userGrant},
			{name: "import_user", operation: "import", catalog: catalogWith(userGranted(false)), planned: userGrant},
		},
		"grant": grantTranscripts(),
	}
}

// TestLifecycleTranscripts records the SQL of every lifecycle case and its statement-shape variants.
// The transcripts characterize current behavior; a refactor must leave them byte-identical.
func TestLifecycleTranscripts(t *testing.T) {
	variants := lifecycleVariants()
	names := map[string]bool{}
	for _, test := range lifecycleCases() {
		names[test.name] = true
		group := "lifecycle/" + resourceTypeName(test.new)
		t.Run(group, func(t *testing.T) {
			runTranscripts(t, group, test.new, append(standardTranscripts(test), variants[test.name]...))
		})
	}
	for name := range variants {
		assert.True(t, names[name], "variants for unknown lifecycle case %q", name)
	}
}

// TestUserTranscripts records user creation, password rotation, capability changes, deletion, and import.
func TestUserTranscripts(t *testing.T) {
	user := grafanaUser()
	admin := user
	admin.Superuser, admin.CreateDB = types.BoolValue(true), types.BoolValue(true)
	rotated := user
	rotated.PasswordVersion = types.Int64Value(1)
	secret := func(model userModel, password string) userModel {
		model.Password = types.StringValue(password)
		return model
	}
	existing := func(c *catalog) func() sqlclient.Client {
		return func() sqlclient.Client { return c }
	}
	runTranscripts(t, "lifecycle/user", newUserResource, []transcriptCase{
		{name: "create", operation: "create", catalog: existing(&catalog{}), planned: user, config: secret(user, "InitialPass123")},
		{name: "create_superuser_createdb", operation: "create", catalog: existing(&catalog{}), planned: admin, config: secret(admin, "AdminPass123")},
		{name: "read", operation: "read", catalog: existing(&catalog{user: true}), prior: user},
		{name: "rotate_password", operation: "update", catalog: existing(&catalog{user: true}), prior: user, planned: rotated, config: secret(rotated, `Rotated'Pass\456`)},
		{name: "grant_capabilities", operation: "update", catalog: existing(&catalog{user: true}), prior: user, planned: admin},
		{name: "revoke_capabilities", operation: "update", catalog: existing(&catalog{user: true, superuser: true, createDB: true}), prior: admin, planned: user},
		{name: "delete", operation: "delete", catalog: existing(&catalog{user: true}), prior: user},
		{name: "import", operation: "import", catalog: existing(&catalog{}), planned: user, config: secret(user, "InitialPass123")},
	})
}

// commentCatalog emulates one comment target whose annotation the statements change.
type commentCatalog struct {
	// text is the current annotation; empty means none.
	text string
}

// commentUnescaper reverses sqlclient.Literal for the fake's stored annotation.
var commentUnescaper = strings.NewReplacer("''", "'", `\\`, `\`)

// Query answers the database check and comment read, and applies COMMENT ON.
func (c *commentCatalog) Query(_ context.Context, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT database_name"):
		return []sqlclient.Row{{"database_name": parameters["database"]}}, nil
	case strings.HasPrefix(sql, "SELECT COALESCE"):
		return []sqlclient.Row{{"text": c.text}}, nil
	case strings.HasPrefix(sql, "COMMENT ON") && strings.HasSuffix(sql, " IS NULL"):
		c.text = ""
	case strings.HasPrefix(sql, "COMMENT ON"):
		text, err := literal(sql, 0)
		if err != nil {
			return nil, err
		}
		c.text = commentUnescaper.Replace(text)
	default:
		return nil, fmt.Errorf("unexpected SQL: %s", sql)
	}
	return nil, nil
}

// TestCommentTranscripts records comment creation, update, and removal for every object kind.
func TestCommentTranscripts(t *testing.T) {
	text := func(value string) func() sqlclient.Client {
		return func() sqlclient.Client { return &commentCatalog{text: value} }
	}
	var cases []transcriptCase
	for _, target := range []struct{ kind, schema, name, column string }{
		{"DATABASE", "", "analytics", ""},
		{"SCHEMA", "", "serving", ""},
		{"TABLE", "serving", "table", ""},
		{"VIEW", "serving", "view", ""},
		{"COLUMN", "serving", "table", "column"},
	} {
		model := commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue(target.kind), ObjectName: types.StringValue(target.name), SchemaName: types.StringNull(), ColumnName: types.StringNull(), Text: types.StringValue("note")}
		if target.schema != "" {
			model.SchemaName = types.StringValue(target.schema)
		}
		if target.column != "" {
			model.ColumnName = types.StringValue(target.column)
		}
		revised := model
		revised.Text = types.StringValue(`Owner's \notes`)
		kind := strings.ToLower(target.kind)
		cases = append(cases,
			transcriptCase{name: "create_" + kind, operation: "create", catalog: text(""), planned: model},
			transcriptCase{name: "update_" + kind, operation: "update", catalog: text("note"), prior: model, planned: revised},
			transcriptCase{name: "delete_" + kind, operation: "delete", catalog: text(`Owner's \notes`), prior: revised},
		)
		if target.kind == "SCHEMA" {
			cleared := model
			cleared.Text = types.StringValue("")
			cases = append(cases,
				transcriptCase{name: "read_" + kind, operation: "read", catalog: text("note"), prior: model},
				transcriptCase{name: "clear_" + kind, operation: "update", catalog: text("note"), prior: model, planned: cleared},
				transcriptCase{name: "unchanged_" + kind, operation: "update", catalog: text("note"), prior: model, planned: model},
			)
		}
		if target.kind == "COLUMN" {
			cases = append(cases, transcriptCase{name: "import_" + kind, operation: "import", catalog: text(""), planned: model})
		}
	}
	runTranscripts(t, "lifecycle/comment", newCommentResource, cases)
}

// privilegeTranscript describes one permission tuple and the privilege sets around an operation.
type privilegeTranscript struct {
	// name is the transcript file name.
	name string
	// operation is the lifecycle step under test.
	operation string
	// fields overrides the type's base tuple fields; an empty value removes a field.
	fields map[string]string
	// current is the catalog's privilege set before the operation.
	current []string
	// prior is the state's privilege set for read, update, and delete.
	prior []string
	// desired is the planned privilege set for create and update.
	desired []string
}

// TestPrivilegeTranscripts records exact-set reconciliation for every privilegeResource type and statement shape.
func TestPrivilegeTranscripts(t *testing.T) {
	for _, test := range []struct {
		factory    func() resource.Resource
		fields     map[string]string
		privileges []string
		variants   []privilegeTranscript
	}{
		{
			factory:    newObjectGrantResource,
			fields:     map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "table", "object_type": "TABLE", "grantee_type": "GROUP", "grantee": "readers"},
			privileges: []string{"SELECT", "INSERT"},
			variants: []privilegeTranscript{
				{name: "create_database_role", operation: "create", fields: map[string]string{"schema_name": "", "object_name": "", "object_type": "DATABASE", "grantee_type": "ROLE"}, desired: []string{"TEMPORARY"}},
				{name: "create_schema_user", operation: "create", fields: map[string]string{"object_name": "", "object_type": "SCHEMA", "grantee_type": "USER", "grantee": "loader"}, desired: []string{"USAGE"}},
				{name: "create_table_public", operation: "create", fields: map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, desired: []string{"SELECT"}},
				{name: "create_quoted_identifiers", operation: "create", fields: map[string]string{"database_name": `odd"database`, "schema_name": `odd"schema`, "object_name": `odd"table`, "grantee": `odd"readers`}, desired: []string{"SELECT"}},
			},
		},
		{
			factory:    newDefaultPrivilegesResource,
			fields:     map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee_type": "ROLE", "grantee": "readers"},
			privileges: []string{"SELECT", "INSERT"},
			variants: []privilegeTranscript{
				{name: "create_without_schema", operation: "create", fields: map[string]string{"schema_name": ""}, desired: []string{"SELECT"}},
				{name: "delete_without_schema", operation: "delete", fields: map[string]string{"schema_name": ""}, current: []string{"SELECT"}, prior: []string{"SELECT"}},
				{name: "create_functions_user", operation: "create", fields: map[string]string{"object_type": "FUNCTIONS", "grantee_type": "USER", "grantee": "analyst"}, desired: []string{"EXECUTE"}},
				{name: "create_procedures_public", operation: "create", fields: map[string]string{"schema_name": "", "object_type": "PROCEDURES", "grantee_type": "PUBLIC", "grantee": "public"}, desired: []string{"EXECUTE"}},
				{name: "create_group", operation: "create", fields: map[string]string{"grantee_type": "GROUP"}, desired: []string{"SELECT"}},
			},
		},
		{
			factory:    newSystemGrantResource,
			fields:     map[string]string{"role": "operators"},
			privileges: []string{"CREATE USER", "CREATE ROLE"},
		},
		{
			factory:    newAssumeroleGrantResource,
			fields:     map[string]string{"iam_role_arn": "default", "grantee_type": "ROLE", "grantee": "readers"},
			privileges: []string{"COPY", "UNLOAD"},
			variants: []privilegeTranscript{
				{name: "create_arn", operation: "create", fields: map[string]string{"iam_role_arn": "arn:aws:iam::123456789012:role/loader"}, desired: []string{"COPY"}},
				{name: "delete_arn", operation: "delete", fields: map[string]string{"iam_role_arn": "arn:aws:iam::123456789012:role/loader"}, current: []string{"COPY"}, prior: []string{"COPY"}},
				{name: "create_default_public", operation: "create", fields: map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, desired: []string{"EXTERNAL FUNCTION"}},
				{name: "create_arn_user", operation: "create", fields: map[string]string{"iam_role_arn": "arn:aws:iam::123456789012:role/loader", "grantee_type": "USER", "grantee": "analyst"}, desired: []string{"UNLOAD"}},
				{name: "create_default_group", operation: "create", fields: map[string]string{"grantee_type": "GROUP"}, desired: []string{"CREATE MODEL"}},
			},
		},
	} {
		group := "privilege/" + resourceTypeName(test.factory)
		t.Run(group, func(t *testing.T) {
			first, second := test.privileges[0], test.privileges[1]
			flows := append([]privilegeTranscript{
				{name: "create", operation: "create", desired: []string{first}},
				{name: "read", operation: "read", current: []string{first}, prior: []string{first}},
				{name: "update_replace", operation: "update", current: []string{first}, prior: []string{first}, desired: []string{second}},
				{name: "update_add", operation: "update", current: []string{first}, prior: []string{first}, desired: []string{first, second}},
				{name: "update_unchanged", operation: "update", current: []string{first}, prior: []string{first}, desired: []string{first}},
				{name: "delete", operation: "delete", current: []string{first, second}, prior: []string{first, second}},
				{name: "import", operation: "import", desired: []string{first}},
			}, test.variants...)
			var cases []transcriptCase
			for _, flow := range flows {
				fields := maps.Clone(test.fields)
				for key, value := range flow.fields {
					fields[key] = value
					if value == "" {
						delete(fields, key)
					}
				}
				r := test.factory().(*privilegeResource)
				current := flow.current
				cases = append(cases, transcriptCase{
					name: flow.name, operation: flow.operation,
					catalog: func() sqlclient.Client {
						values := map[string]bool{}
						for _, privilege := range current {
							values[privilege] = true
						}
						return &privilegeCatalog{values: values, grantee: fields["grantee"], kind: strings.ToLower(fields["grantee_type"]), scope: fields["object_type"]}
					},
					prior:   privilegeObject(t, r, fields, flow.prior...),
					planned: privilegeObject(t, r, fields, flow.desired...),
				})
			}
			runTranscripts(t, group, test.factory, cases)
		})
	}
}
