package provider

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOptionGrantTestResource enables grant options on the object_grant contract, so the shared reconciliation is
// covered before a registered type opts in.
func newOptionGrantTestResource() resource.Resource {
	r := newObjectGrantResource().(*privilegeResource)
	r.name, r.grantOptions = "option_grant_test", true
	return r
}

// newHookedOptionGrantTestResource decides option eligibility through optionRecipient, as a contract that names its
// user in another attribute than grantee_type does; here only the grantee analyst counts as a user.
func newHookedOptionGrantTestResource() resource.Resource {
	r := newOptionGrantTestResource().(*privilegeResource)
	r.optionRecipient = func(data types.Object) bool { return objectString(data, "grantee") == "analyst" }
	return r
}

// newPolicyGrantTestResource grants table lookups to an RLS policy through the recipient override, the shape
// GRANT SELECT ON TABLE … TO RLS POLICY uses.
func newPolicyGrantTestResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["database_name"] = privilegeString("Local database containing the lookup table.", false)
	attributes["schema_name"] = privilegeString("Schema containing the lookup table.", false)
	attributes["object_name"] = privilegeString("Lookup table name.", false)
	attributes["policy"] = privilegeString("RLS policy receiving the lookup privilege.", false)
	return &privilegeResource{
		name: "policy_grant_test", attributes: attributes, fields: []string{"database_name", "schema_name", "object_name", "policy"},
		grantOptions: true,
		prepare: func(data types.Object) (privilegeTarget, error) {
			database, schemaName, name := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name")
			checks, err := newCatalogChecks(localDatabaseQuery(database), objectGrantTableQuery(database, schemaName, name))
			if err != nil {
				return privilegeTarget{}, err
			}
			object := sqlclient.Kw("ON", "TABLE").Qualified(database, schemaName, name)
			return privilegeTarget{
				database: database, checks: checks, allowed: []sqlclient.Keyword{"SELECT"}, grant: grantSpec{object: object},
				query:  catalogCheck{sql: sqlclient.Stmt("SHOW GRANTS").Append(object).String()},
				filter: func(row sqlclient.Row) bool { return row["identity_name"] == objectString(data, "policy") },
			}, nil
		},
		recipient: func(data types.Object) (sqlclient.Statement, error) {
			policy := objectString(data, "policy")
			if policy == "" {
				return sqlclient.Statement{}, fmt.Errorf("policy is required")
			}
			return sqlclient.Fragment().KwIdent("RLS POLICY", policy), nil
		},
	}
}

// optionGrantFields is a user tuple on one table, the only recipient kind that can hold grant options.
var optionGrantFields = map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "orders", "object_type": "TABLE", "grantee_type": "USER", "grantee": "analyst"}

// policyGrantFields is an RLS policy tuple on one lookup table.
var policyGrantFields = map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "regions", "policy": "region_filter"}

// optionObject builds a permission model with privileges and their grant option subset.
func optionObject(t *testing.T, r *privilegeResource, fields map[string]string, privileges, options []string) types.Object {
	t.Helper()
	return withGrantOptions(privilegeObject(t, r, fields, privileges...), options)
}

// optionCatalog returns a fake holding privileges, of which options carry the grant option.
func optionCatalog(fields map[string]string, privileges, options []string) *privilegeCatalog {
	c := &privilegeCatalog{values: map[string]bool{}, options: map[string]bool{}, grantee: fields["grantee"], kind: "user", scope: fields["object_type"]}
	if fields["policy"] != "" {
		c.grantee, c.kind = fields["policy"], "rls policy"
	}
	for _, privilege := range privileges {
		c.values[privilege] = true
	}
	for _, option := range options {
		c.options[option] = true
	}
	return c
}

// TestGrantOptionSpecSQL pins WITH GRANT OPTION and REVOKE GRANT OPTION FOR for plain and prefixed grants, and
// rejects shapes that have no grant option form.
func TestGrantOptionSpecSQL(t *testing.T) {
	both := func(spec grantSpec, privilege sqlclient.Keyword) func() ([]string, error) {
		return func() ([]string, error) {
			var statements []string
			for _, grant := range []bool{true, false} {
				statement, err := spec.optionStatement(grant, privilege)
				if err != nil {
					return nil, err
				}
				statements = append(statements, statement)
			}
			return statements, nil
		}
	}
	object := sqlclient.Fragment().KwQualified("ON TABLE", `Odd"Database`, "serving", `Odd"Table`)
	checkSQL(t, "privilege_sql/grant_option_spec", []sqlCase{
		{"object_user", both(grantSpec{object: object, grantee: sqlclient.Ident(`Odd"User`)}, "SELECT")},
		{"prefix", both(grantSpec{prefix: sqlclient.Stmt("ALTER DEFAULT PRIVILEGES FOR USER").Ident("loader").OptIdent("IN SCHEMA", "serving"), object: sqlclient.Kw("ON", "TABLES"), grantee: sqlclient.Ident("analyst")}, "INSERT")},
		{"render_override", both(grantSpec{render: func(bool, sqlclient.Keyword) string { return "GRANT ASSUMEROLE" }}, "COPY")},
		{"fixed_option", both(grantSpec{object: object, grantee: sqlclient.Ident("analyst"), option: "WITH GRANT OPTION"}, "SELECT")},
		{"scoped", both(grantSpec{object: sqlclient.Kw("FOR TABLES").KwIdent("IN SCHEMA", "serving").KwIdent("DATABASE", "analytics"), grantee: sqlclient.Ident("analyst"), optionRevoke: scopedOptionRevoke}, "SELECT")},
	})
}

// TestPrivilegeOptionStatementsSQL pins the reconcile order: revoke, downgrade, grant, upgrade.
func TestPrivilegeOptionStatementsSQL(t *testing.T) {
	spec := grantSpec{object: sqlclient.Fragment().KwQualified("ON TABLE", "analytics", "serving", "orders"), grantee: sqlclient.Ident("analyst")}
	allowed := []sqlclient.Keyword{"DELETE", "INSERT", "SELECT", "TRUNCATE", "UPDATE"}
	render := func(spec grantSpec, current, desired privilegeSets) func() ([]string, error) {
		return func() ([]string, error) { return privilegeOptionStatements(spec, allowed, current, desired) }
	}
	sets := func(privileges []string, options ...string) privilegeSets {
		return privilegeSets{privileges: privileges, options: options}
	}
	checkSQL(t, "privilege_sql/option_statements", []sqlCase{
		{"converged", render(spec, sets([]string{"SELECT"}, "SELECT"), sets([]string{"SELECT"}, "SELECT"))},
		{"upgrade", render(spec, sets([]string{"SELECT"}), sets([]string{"SELECT"}, "SELECT"))},
		{"downgrade", render(spec, sets([]string{"SELECT"}, "SELECT"), sets([]string{"SELECT"}))},
		{"grant_new_with_option", render(spec, sets(nil), sets([]string{"INSERT", "SELECT"}, "SELECT"))},
		{"revoke_with_option", render(spec, sets([]string{"SELECT"}, "SELECT"), sets(nil))},
		{"full_order", render(spec, sets([]string{"DELETE", "INSERT", "SELECT"}, "DELETE", "SELECT"), sets([]string{"INSERT", "SELECT", "TRUNCATE", "UPDATE"}, "INSERT", "UPDATE"))},
		{"unsupported_option", render(spec, sets([]string{"SELECT"}), sets([]string{"SELECT"}, "SELECT; DROP TABLE x"))},
		{"render_override", render(grantSpec{render: func(bool, sqlclient.Keyword) string { return "GRANT ASSUMEROLE" }}, sets([]string{"SELECT"}), sets([]string{"SELECT"}, "SELECT"))},
	})
}

// TestPrivilegeGrantOptionSchema exposes grant_option_privileges only on types that opt in, defaulting to none.
func TestPrivilegeGrantOptionSchema(t *testing.T) {
	var plain, options resource.SchemaResponse
	newObjectGrantResource().Schema(context.Background(), resource.SchemaRequest{}, &plain)
	newOptionGrantTestResource().Schema(context.Background(), resource.SchemaRequest{}, &options)
	assert.NotContains(t, plain.Schema.Attributes, "grant_option_privileges")
	assert.NotContains(t, newOptionGrantTestResource().(*privilegeResource).attributes, "grant_option_privileges", "the shared attribute map must stay unchanged")
	require.Contains(t, options.Schema.Attributes, "grant_option_privileges")
	attribute := options.Schema.Attributes["grant_option_privileges"].(schema.SetAttribute)
	assert.True(t, attribute.Optional)
	assert.True(t, attribute.Computed)
	assert.Contains(t, options.Schema.MarkdownDescription, "WITH GRANT OPTION")
	assert.NotContains(t, plain.Schema.MarkdownDescription, "WITH GRANT OPTION")
}

// TestPrivilegeGrantOptionValidation rejects grant options for non-user recipients and outside privileges, both
// during planning and before Create records state.
func TestPrivilegeGrantOptionValidation(t *testing.T) {
	for _, test := range []struct {
		name       string
		factory    func() resource.Resource
		fields     map[string]string
		privileges []string
		options    []string
		err        string
	}{
		{name: "user subset", factory: newOptionGrantTestResource, fields: optionGrantFields, privileges: []string{"INSERT", "SELECT"}, options: []string{"SELECT"}},
		{name: "user without options", factory: newOptionGrantTestResource, fields: optionGrantFields, privileges: []string{"SELECT"}},
		{name: "role", factory: newOptionGrantTestResource, fields: map[string]string{"grantee_type": "ROLE", "grantee": "readers"}, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "group", factory: newOptionGrantTestResource, fields: map[string]string{"grantee_type": "GROUP", "grantee": "readers"}, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "public", factory: newOptionGrantTestResource, fields: map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "not a privilege", factory: newOptionGrantTestResource, fields: optionGrantFields, privileges: []string{"SELECT"}, options: []string{"INSERT"}, err: `grant option privilege "INSERT" is not in privileges`},
		{name: "policy recipient", factory: newPolicyGrantTestResource, fields: policyGrantFields, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "policy without options", factory: newPolicyGrantTestResource, fields: policyGrantFields, privileges: []string{"SELECT"}},
		{name: "hook accepts", factory: newHookedOptionGrantTestResource, fields: map[string]string{"grantee_type": "ROLE", "grantee": "analyst"}, privileges: []string{"SELECT"}, options: []string{"SELECT"}},
		{name: "hook rejects", factory: newHookedOptionGrantTestResource, fields: map[string]string{"grantee": "loader"}, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "invalid recipient", factory: newPolicyGrantTestResource, fields: map[string]string{"policy": ""}, privileges: []string{"SELECT"}, err: "policy is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := test.factory().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				return nil, fmt.Errorf("unexpected SQL %q", sql)
			}))
			fields := maps.Clone(optionGrantFields)
			maps.Copy(fields, test.fields)
			data := optionObject(t, r, fields, test.privileges, test.options)
			err := r.validate(data)
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &validated)
			if test.err == "" {
				require.NoError(t, err)
				assert.False(t, validated.Diagnostics.HasError(), "%v", validated.Diagnostics)
				return
			}
			require.ErrorContains(t, err, test.err)
			assert.True(t, validated.Diagnostics.HasError(), "invalid grant options must be reported during planning")
			state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.True(t, diagnostics.HasError())
			assert.True(t, state.Raw.IsNull(), "invalid grant options must not be recorded in state")
		})
	}
}

// TestPrivilegeGrantOptionRead reports catalog grant options only on types that manage them, and treats a
// privilege as held with the option when any of its rows carries it.
func TestPrivilegeGrantOptionRead(t *testing.T) {
	rows := []sqlclient.Row{
		{"privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE", "admin_option": "false"},
		{"privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE", "admin_option": "true"},
		{"privilege_type": "INSERT", "identity_name": "analyst", "identity_type": "user", "privilege_scope": "TABLE", "admin_option": "f"},
	}
	client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if sql == `SHOW GRANTS ON TABLE "warehouse"."serving"."orders"` {
			return rows, nil
		}
		return []sqlclient.Row{{"name": "exists"}}, nil
	})
	r := newOptionGrantTestResource().(*privilegeResource)
	r.resourceClient = testResourceClient(client)
	data := optionObject(t, r, optionGrantFields, nil, nil)
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"INSERT", "SELECT"}, knownStrings(data.Attributes()["privileges"].(types.Set)))
	assert.Equal(t, []string{"SELECT"}, grantOptionPrivileges(data))

	plain := newObjectGrantResource().(*privilegeResource)
	plain.resourceClient = testResourceClient(client)
	data = privilegeObject(t, plain, optionGrantFields)
	_, _, err = plain.read(context.Background(), &data)
	require.ErrorContains(t, err, "grant options are not managed")
	_, found, err = plain.readPrivileges(context.Background(), &data, false)
	require.NoError(t, err, "unmanaged reads still accept grant options")
	assert.True(t, found)
	assert.NotContains(t, data.Attributes(), "grant_option_privileges")
}

// TestPrivilegeGrantOptionReconcile applies upgrades and downgrades through the fake catalog and verifies the
// re-read covers both sets.
func TestPrivilegeGrantOptionReconcile(t *testing.T) {
	for _, test := range []struct {
		name                                         string
		privileges, options, desired, desiredOptions []string
		writes                                       []string
	}{
		{
			name: "upgrade", privileges: []string{"SELECT"}, desired: []string{"SELECT"}, desiredOptions: []string{"SELECT"},
			writes: []string{`GRANT SELECT ON TABLE "warehouse"."serving"."orders" TO "analyst" WITH GRANT OPTION`},
		},
		{
			name: "downgrade", privileges: []string{"SELECT"}, options: []string{"SELECT"}, desired: []string{"SELECT"},
			writes: []string{`REVOKE GRANT OPTION FOR SELECT ON TABLE "warehouse"."serving"."orders" FROM "analyst"`},
		},
		{
			name: "replace", privileges: []string{"SELECT"}, options: []string{"SELECT"}, desired: []string{"INSERT"}, desiredOptions: []string{"INSERT"},
			writes: []string{
				`REVOKE SELECT ON TABLE "warehouse"."serving"."orders" FROM "analyst"`,
				`GRANT INSERT ON TABLE "warehouse"."serving"."orders" TO "analyst" WITH GRANT OPTION`,
			},
		},
		{name: "converged", privileges: []string{"SELECT"}, options: []string{"SELECT"}, desired: []string{"SELECT"}, desiredOptions: []string{"SELECT"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newOptionGrantTestResource().(*privilegeResource)
			c := optionCatalog(optionGrantFields, test.privileges, test.options)
			r.resourceClient = testResourceClient(c)
			require.NoError(t, r.reconcile(context.Background(), optionObject(t, r, optionGrantFields, test.desired, test.desiredOptions)))
			assert.Equal(t, test.writes, c.writes)
		})
	}
	t.Run("option not converged", func(t *testing.T) {
		r := newOptionGrantTestResource().(*privilegeResource)
		c := optionCatalog(optionGrantFields, []string{"SELECT"}, nil)
		c.admin = "t"
		r.resourceClient = testResourceClient(c)
		require.ErrorContains(t, r.reconcile(context.Background(), optionObject(t, r, optionGrantFields, []string{"SELECT"}, nil)), "did not converge")
	})
}

// TestPrivilegeGrantOptionLifecycle runs the shared lifecycle checks on a type with grant options enabled.
func TestPrivilegeGrantOptionLifecycle(t *testing.T) {
	exercisePrivilege(t, newOptionGrantTestResource, optionGrantFields, []string{"SELECT", "INSERT"})
}

// TestPrivilegeOptionTranscripts records grant option flows and the policy recipient override.
func TestPrivilegeOptionTranscripts(t *testing.T) {
	// flow is one operation with the catalog, prior, and desired privilege and grant option sets around it.
	type flow struct {
		name, operation         string
		current, currentOptions []string
		prior, priorOptions     []string
		desired, desiredOptions []string
	}
	record := func(t *testing.T, factory func() resource.Resource, fields map[string]string, flows []flow) {
		t.Helper()
		var cases []transcriptCase
		for _, flow := range flows {
			r := factory().(*privilegeResource)
			cases = append(cases, transcriptCase{
				name: flow.name, operation: flow.operation,
				catalog: func() sqlclient.Client { return optionCatalog(fields, flow.current, flow.currentOptions) },
				prior:   optionObject(t, r, fields, flow.prior, flow.priorOptions),
				planned: optionObject(t, r, fields, flow.desired, flow.desiredOptions),
			})
		}
		runTranscripts(t, "privilege/"+resourceTypeName(factory), factory, cases)
	}
	selectOnly := []string{"SELECT"}
	t.Run("options", func(t *testing.T) {
		record(t, newOptionGrantTestResource, optionGrantFields, []flow{
			{name: "create_with_option", operation: "create", desired: []string{"INSERT", "SELECT"}, desiredOptions: selectOnly},
			{name: "read_with_option", operation: "read", current: selectOnly, currentOptions: selectOnly, prior: selectOnly, priorOptions: selectOnly},
			{name: "upgrade", operation: "update", current: selectOnly, prior: selectOnly, desired: selectOnly, desiredOptions: selectOnly},
			{name: "downgrade", operation: "update", current: selectOnly, currentOptions: selectOnly, prior: selectOnly, priorOptions: selectOnly, desired: selectOnly},
			{name: "replace_with_option", operation: "update", current: selectOnly, currentOptions: selectOnly, prior: selectOnly, priorOptions: selectOnly, desired: []string{"INSERT"}, desiredOptions: []string{"INSERT"}},
			{name: "delete_with_option", operation: "delete", current: []string{"INSERT", "SELECT"}, currentOptions: selectOnly, prior: []string{"INSERT", "SELECT"}, priorOptions: selectOnly},
			{name: "import_with_option", operation: "import", desired: selectOnly, desiredOptions: selectOnly},
		})
	})
	t.Run("policy", func(t *testing.T) {
		record(t, newPolicyGrantTestResource, policyGrantFields, []flow{
			{name: "create", operation: "create", desired: selectOnly},
			{name: "read", operation: "read", current: selectOnly, prior: selectOnly},
			{name: "delete", operation: "delete", current: selectOnly, prior: selectOnly},
			{name: "import", operation: "import", desired: selectOnly},
		})
	})
}
