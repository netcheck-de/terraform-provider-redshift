package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// grantSQL renders every statement a scoped grant can send: the database type and recipient checks, the schema
// check when a schema is set, the SHOW GRANTS read, and one GRANT and one REVOKE of privilege.
func grantSQL(data grantModel, privilege sqlclient.Keyword) ([]string, error) {
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
	return append(statements, readGrantStatement(data), spec.statement(true, privilege), spec.statement(false, privilege)), nil
}

// TestGrantSQL pins the ON and FOR clauses of every scope and both recipient kinds.
func TestGrantSQL(t *testing.T) {
	render := func(database, schema, role, datashare, scope string, privilege sqlclient.Keyword) func() ([]string, error) {
		optional := func(value string) types.String {
			if value == "" {
				return types.StringNull()
			}
			return types.StringValue(value)
		}
		data := grantModel{
			DatabaseName: types.StringValue(database), SchemaName: optional(schema), Role: optional(role), Datashare: optional(datashare),
			Scope: types.StringValue(scope), Privileges: types.SetValueMust(types.StringType, nil),
		}
		return func() ([]string, error) { return grantSQL(data, privilege) }
	}
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
		{"datashare_schema", render("analytics", "serving", "", "producer", "SCHEMA", "USAGE")},
		{"datashare_tables", render("analytics", "serving", "", "producer", "TABLES", "SELECT")},
		{"quoted_role", render(`Odd"Database`, `Odd"Schema`, `example:Odd"Role`, "", "SCHEMA", "USAGE")},
		{"quoted_role_scope", render(`Odd"Database`, `Odd"Schema`, `example:Odd"Role`, "", "TABLES", "SELECT")},
		{"quoted_datashare", render(`Odd"Database`, `Odd"Schema`, "", `Odd"Share`, "TABLES", "SELECT")},
		{"schema_without_schema_name", render("analytics", "", "readers", "", "SCHEMA", "USAGE")},
		{"database_with_schema_name", render("analytics", "serving", "readers", "", "DATABASE", "USAGE")},
		{"without_recipient", render("analytics", "", "", "", "DATABASE", "USAGE")},
		{"datashare_database_scope", render("analytics", "serving", "", "producer", "FUNCTIONS", "EXECUTE")},
		{"empty_database_name", render("", "", "readers", "", "DATABASE", "USAGE")},
	})
}
