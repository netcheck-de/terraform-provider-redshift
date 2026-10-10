package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantSQL renders every statement a scoped grant can send: the database type and recipient checks, the schema
// check when a schema is set, the SHOW GRANTS read, one GRANT and one REVOKE of privilege, and for a user the grant
// option upgrade and downgrade of it.
func grantSQL(data grantModel, privilege sqlclient.Keyword) ([]string, error) {
	data = grantTestModel(data)
	data.Privileges = types.SetValueMust(types.StringType, []attr.Value{types.StringValue(string(privilege))})
	if err := data.validate(); err != nil {
		return nil, err
	}
	spec, err := data.spec()
	if err != nil {
		return nil, err
	}
	queries := []sqlclient.Query{grantDatabaseTypeQuery(data.DatabaseName.ValueString()), grantRecipientQuery(data)}
	if schema := knownString(data.SchemaName); schema != "" {
		queries = append(queries, privilegeSchemaQuery(data.DatabaseName.ValueString(), schema))
	}
	var statements []string
	for _, query := range queries {
		sql, _, err := query.Build()
		if err != nil {
			return nil, err
		}
		statements = append(statements, sql)
	}
	statements = append(statements, readGrantStatement(data), spec.statement(true, privilege), spec.statement(false, privilege))
	if knownString(data.User) == "" {
		return statements, nil
	}
	for _, grant := range []bool{true, false} {
		statement, err := spec.optionStatement(grant, privilege)
		if err != nil {
			return nil, err
		}
		statements = append(statements, statement)
	}
	return statements, nil
}

// TestGrantSQL pins the ON and FOR clauses of every scope and recipient kind, including grant options for users.
func TestGrantSQL(t *testing.T) {
	optional := func(value string) types.String {
		if value == "" {
			return types.StringNull()
		}
		return types.StringValue(value)
	}
	model := func(database, schema, scope string) grantModel {
		return grantModel{DatabaseName: types.StringValue(database), SchemaName: optional(schema), Scope: types.StringValue(scope)}
	}
	render := func(database, schema, role, datashare, scope string, privilege sqlclient.Keyword) func() ([]string, error) {
		data := model(database, schema, scope)
		data.Role, data.Datashare = optional(role), optional(datashare)
		return func() ([]string, error) { return grantSQL(data, privilege) }
	}
	user := func(database, schema, name, scope string, privilege sqlclient.Keyword) func() ([]string, error) {
		data := model(database, schema, scope)
		data.User = optional(name)
		return func() ([]string, error) { return grantSQL(data, privilege) }
	}
	options := func(data grantModel, privileges, options []string) func() ([]string, error) {
		return func() ([]string, error) {
			data = grantTestModel(data)
			data.Privileges = types.SetValueMust(types.StringType, grantStringValues(privileges))
			data.GrantOptionPrivileges = types.SetValueMust(types.StringType, grantStringValues(options))
			if err := data.validate(); err != nil {
				return nil, err
			}
			spec, err := data.spec()
			if err != nil {
				return nil, err
			}
			return privilegeOptionStatements(spec, data.allowed(), privilegeSets{}, privilegeSets{privileges: privileges, options: options})
		}
	}
	analyst := model("analytics", "", "TABLES")
	analyst.User = types.StringValue("analyst")
	readers := model("analytics", "", "TABLES")
	readers.Role = types.StringValue("readers")
	checkSQL(t, "grant", []sqlCase{
		{"database", render("analytics", "", "readers", "", "DATABASE", "TEMPORARY")},
		{"schemas", render("analytics", "", "readers", "", "SCHEMAS", "USAGE")},
		{"schema", render("analytics", "serving", "readers", "", "SCHEMA", "CREATE")},
		{"tables_in_database", render("analytics", "", "readers", "", "TABLES", "SELECT")},
		{"tables_in_schema", render("analytics", "serving", "readers", "", "TABLES", "SELECT")},
		{"functions_in_database", render("analytics", "", "readers", "", "FUNCTIONS", "EXECUTE")},
		{"functions_in_schema", render("analytics", "serving", "readers", "", "FUNCTIONS", "EXECUTE")},
		{"procedures_in_database", render("analytics", "", "readers", "", "PROCEDURES", "EXECUTE")},
		{"procedures_in_schema", render("analytics", "serving", "readers", "", "PROCEDURES", "EXECUTE")},
		{"languages", render("analytics", "", "readers", "", "LANGUAGES", "USAGE")},
		{"copy_jobs", render("analytics", "", "readers", "", "COPY JOBS", "CREATE")},
		{"templates_in_database", render("analytics", "", "readers", "", "TEMPLATES", "USAGE")},
		{"templates_in_schema", render("analytics", "serving", "readers", "", "TEMPLATES", "ALTER")},
		{"datashare_schema", render("analytics", "serving", "", "producer", "SCHEMA", "USAGE")},
		{"datashare_tables", render("analytics", "serving", "", "producer", "TABLES", "SELECT")},
		{"user_database", user("analytics", "", "analyst", "DATABASE", "CREATE")},
		{"user_schema", user("analytics", "serving", "analyst", "SCHEMA", "USAGE")},
		{"user_tables_in_database", user("analytics", "", "analyst", "TABLES", "SELECT")},
		{"user_tables_in_schema", user("analytics", "serving", "analyst", "TABLES", "INSERT")},
		{"user_languages", user("analytics", "", "analyst", "LANGUAGES", "USAGE")},
		{"user_copy_jobs", user("analytics", "", "analyst", "COPY JOBS", "DROP")},
		{"user_templates_in_schema", user("analytics", "serving", "analyst", "TEMPLATES", "USAGE")},
		{"quoted_role", render(`Odd"Database`, `Odd"Schema`, `example:Odd"Role`, "", "SCHEMA", "USAGE")},
		{"quoted_role_scope", render(`Odd"Database`, `Odd"Schema`, `example:Odd"Role`, "", "TABLES", "SELECT")},
		{"quoted_datashare", render(`Odd"Database`, `Odd"Schema`, "", `Odd"Share`, "TABLES", "SELECT")},
		{"quoted_user_scope", user(`Odd"Database`, `Odd"Schema`, `Odd"User`, "TABLES", "SELECT")},
		{"quoted_user_database", user(`Odd"Database`, "", `Odd"User`, "DATABASE", "USAGE")},
		{"schema_without_schema_name", render("analytics", "", "readers", "", "SCHEMA", "USAGE")},
		{"database_with_schema_name", render("analytics", "serving", "readers", "", "DATABASE", "USAGE")},
		{"languages_with_schema_name", render("analytics", "serving", "readers", "", "LANGUAGES", "USAGE")},
		{"copy_jobs_with_schema_name", render("analytics", "serving", "readers", "", "COPY JOBS", "CREATE")},
		{"without_recipient", render("analytics", "", "", "", "DATABASE", "USAGE")},
		{"role_and_user", func() ([]string, error) {
			data := model("analytics", "", "DATABASE")
			data.Role, data.User = types.StringValue("readers"), types.StringValue("analyst")
			return grantSQL(data, "USAGE")
		}},
		{"datashare_database_scope", render("analytics", "serving", "", "producer", "FUNCTIONS", "EXECUTE")},
		{"datashare_wrong_privilege", render("analytics", "serving", "", "producer", "SCHEMA", "CREATE")},
		{"empty_database_name", render("", "", "readers", "", "DATABASE", "USAGE")},
		{"unsupported_scope", render("analytics", "", "readers", "", "MODELS", "USAGE")},
		{"scope_privilege_mismatch", render("analytics", "", "readers", "", "LANGUAGES", "EXECUTE")},
		{"database_drop", render("analytics", "", "readers", "", "DATABASE", "DROP")},
		{"option_user", options(analyst, []string{"INSERT", "SELECT"}, []string{"SELECT"})},
		{"option_role", options(readers, []string{"SELECT"}, []string{"SELECT"})},
		{"option_outside_privileges", options(analyst, []string{"SELECT"}, []string{"INSERT"})},
	})
}

// TestGrantSpecOptionRevoke selects the documented grant option revoke: scoped FOR forms omit FOR after GRANT
// OPTION, while ON forms keep the object form.
func TestGrantSpecOptionRevoke(t *testing.T) {
	for scope, expected := range map[string]sqlclient.Keyword{"DATABASE": "", "TABLES": scopedOptionRevoke, "SCHEMAS": scopedOptionRevoke, "LANGUAGES": scopedOptionRevoke, "COPY JOBS": scopedOptionRevoke, "TEMPLATES": scopedOptionRevoke} {
		t.Run(scope, func(t *testing.T) {
			data := grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringNull(), Role: types.StringValue("readers"), Datashare: types.StringNull(), Scope: types.StringValue(scope)}
			spec, err := data.spec()
			require.NoError(t, err)
			assert.Equal(t, expected, spec.optionRevoke)
		})
	}
}

// TestGrantScopePrivileges keeps the schema validator, the scope list, and the per-scope allowlists in step.
func TestGrantScopePrivileges(t *testing.T) {
	assert.Len(t, grantScopePrivileges, len(grantScopes))
	for _, scope := range grantScopes {
		assert.NotEmpty(t, grantScopePrivileges[scope], scope)
		for _, privilege := range grantScopePrivileges[scope] {
			assert.True(t, privilegeAllowed(scopedPrivileges, string(privilege)), "%s %s", scope, privilege)
		}
	}
	assert.ElementsMatch(t, privilegeNames(scopedPrivileges), grantScopeNames())
}
