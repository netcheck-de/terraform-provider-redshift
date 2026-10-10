package provider

import (
	"maps"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObjectGrantSQL pins the checks, catalog read, and GRANT/REVOKE statements of every object kind.
func TestObjectGrantSQL(t *testing.T) {
	table := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "orders", "object_type": "TABLE", "grantee_type": "GROUP", "grantee": "readers"}
	render := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(table)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newObjectGrantResource, fields, privilege)
	}
	routine := func(kind, name, arguments string) map[string]string {
		return map[string]string{"object_type": kind, "object_name": name, "arguments": arguments, "grantee_type": "USER", "grantee": "analyst"}
	}
	snapshot := func(kind string) map[string]string {
		return map[string]string{"object_type": kind, "object_name": "", "grantee_type": "ROLE", "grantee": "readers"}
	}
	options := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		return func() ([]string, error) {
			fields := maps.Clone(table)
			maps.Copy(fields, changes)
			r := newObjectGrantResource().(*privilegeResource)
			target, err := r.prepare(privilegeObject(t, r, fields))
			if err != nil {
				return nil, err
			}
			var statements []string
			for _, grant := range []bool{true, false} {
				statement, err := target.grant.optionStatement(grant, privilege)
				if err != nil {
					return nil, err
				}
				statements = append(statements, statement)
			}
			return statements, nil
		}
	}
	checkSQL(t, "object_grant", []sqlCase{
		{"table_group", render(nil, "SELECT")},
		{"table_public", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, "TRUNCATE")},
		{"schema_user", render(map[string]string{"object_type": "SCHEMA", "object_name": "", "grantee_type": "USER", "grantee": "loader"}, "USAGE")},
		{"database_role", render(map[string]string{"object_type": "DATABASE", "schema_name": "", "object_name": "", "grantee_type": "ROLE", "grantee": "readers"}, "TEMPORARY")},
		{"quoted_identifiers", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_name": `Odd"Table`, "grantee_type": "ROLE", "grantee": `Odd"Role`}, "SELECT")},
		{"function_with_arguments", render(routine("FUNCTION", "f_score", "INT4, VarChar(10), numeric(10,2)"), "EXECUTE")},
		{"function_without_arguments", render(routine("FUNCTION", "f_now", ""), "EXECUTE")},
		{"procedure_with_arguments", render(routine("PROCEDURE", "sp_load", "bigint, timestamp"), "EXECUTE")},
		{"procedure_public", render(map[string]string{"object_type": "PROCEDURE", "object_name": "sp_load", "grantee_type": "PUBLIC", "grantee": "public"}, "EXECUTE")},
		{"quoted_routine", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_type": "FUNCTION", "object_name": `Odd"Function`, "arguments": "text", "grantee_type": "USER", "grantee": `Odd"User`}, "EXECUTE")},
		{"all_tables", render(snapshot("ALL TABLES"), "SELECT")},
		{"all_functions", render(snapshot("ALL FUNCTIONS"), "EXECUTE")},
		{"all_procedures", render(snapshot("ALL PROCEDURES"), "EXECUTE")},
		{"quoted_all_tables", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_type": "ALL TABLES", "object_name": "", "grantee_type": "USER", "grantee": `Odd"User`}, "INSERT")},
		{"option_table_user", options(map[string]string{"grantee_type": "USER", "grantee": `Odd"User`}, "SELECT")},
		{"option_function_user", options(routine("FUNCTION", "f_score", "integer"), "EXECUTE")},
		{"option_all_tables_user", options(map[string]string{"object_type": "ALL TABLES", "object_name": "", "grantee_type": "USER", "grantee": "analyst"}, "SELECT")},
		{"database_with_schema", render(map[string]string{"object_type": "DATABASE", "object_name": ""}, "TEMPORARY")},
		{"schema_without_schema_name", render(map[string]string{"object_type": "SCHEMA", "schema_name": "", "object_name": ""}, "USAGE")},
		{"schema_with_object_name", render(map[string]string{"object_type": "SCHEMA"}, "USAGE")},
		{"table_without_object_name", render(map[string]string{"object_name": ""}, "SELECT")},
		{"function_without_object_name", render(routine("FUNCTION", "", "integer"), "EXECUTE")},
		{"all_tables_with_object_name", render(map[string]string{"object_type": "ALL TABLES"}, "SELECT")},
		{"all_functions_without_schema", render(map[string]string{"object_type": "ALL FUNCTIONS", "schema_name": "", "object_name": ""}, "EXECUTE")},
		{"table_with_arguments", render(map[string]string{"arguments": "integer"}, "SELECT")},
		{"unsupported_argument_type", render(routine("FUNCTION", "f_score", "integer, money"), "EXECUTE")},
		{"unsupported_object_type", render(map[string]string{"object_type": "VIEW"}, "SELECT")},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"}, "SELECT")},
		{"empty_database_name", render(map[string]string{"object_type": "DATABASE", "database_name": "", "schema_name": "", "object_name": ""}, "TEMPORARY")},
	})
}

// TestObjectGrantSignature canonicalizes aliases, case, spacing, and modifiers into the catalog spelling, and keeps
// commas inside a type's parentheses.
func TestObjectGrantSignature(t *testing.T) {
	for arguments, expected := range map[string]sqlclient.Keyword{
		"":                                    "",
		"  ":                                  "",
		"int":                                 "integer",
		"INT4,  VarChar(10)":                  "integer, character varying",
		"numeric(10,2), decimal , float8":     "numeric, numeric, double precision",
		"timestamptz,bool, character varying": "timestamp with time zone, boolean, character varying",
	} {
		signature, err := objectGrantSignature(arguments)
		require.NoError(t, err, arguments)
		assert.Equal(t, expected, signature, arguments)
	}
	for _, arguments := range []string{"integer,", "money", "varchar(10", "integer; DROP TABLE x"} {
		_, err := objectGrantSignature(arguments)
		require.Error(t, err, arguments)
	}
}

// TestObjectGrantPrivileges keeps the object type list and its allowlists in step.
func TestObjectGrantPrivileges(t *testing.T) {
	assert.Len(t, objectGrantPrivileges, len(objectGrantKinds))
	for _, kind := range objectGrantKinds {
		assert.NotEmpty(t, objectGrantPrivileges[kind], kind)
	}
}
