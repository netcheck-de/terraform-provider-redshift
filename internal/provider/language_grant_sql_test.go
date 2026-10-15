package provider

import (
	"maps"
	"testing"
)

// languageGrantBaseFields is a role tuple on plpgsql in the fixture database.
var languageGrantBaseFields = map[string]string{"database_name": "warehouse", "language_name": "PLPGSQL", "grantee": "example:readers", "grantee_type": "ROLE"}

// TestLanguageGrantSQL pins the checks, catalog read, and GRANT/REVOKE USAGE ON LANGUAGE statements, including the
// grant option forms a user grantee receives.
func TestLanguageGrantSQL(t *testing.T) {
	render := func(changes map[string]string) func() ([]string, error) {
		fields := maps.Clone(languageGrantBaseFields)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newLanguageGrantResource, fields, "USAGE")
	}
	options := func(changes map[string]string) func() ([]string, error) {
		return func() ([]string, error) {
			statements, err := render(changes)()
			if err != nil {
				return nil, err
			}
			r := newLanguageGrantResource().(*privilegeResource)
			fields := maps.Clone(languageGrantBaseFields)
			maps.Copy(fields, changes)
			target, err := r.prepare(privilegeObject(t, r, fields))
			if err != nil {
				return nil, err
			}
			for _, grant := range []bool{true, false} {
				statement, err := target.grant.optionStatement(grant, languageGrantPrivileges[0])
				if err != nil {
					return nil, err
				}
				statements = append(statements, statement)
			}
			return statements, nil
		}
	}
	checkSQL(t, "language_grant", []sqlCase{
		{"plpgsql_role", render(nil)},
		{"sql_user_with_grant_option", options(map[string]string{"language_name": "sql", "grantee_type": "USER", "grantee": "analyst"})},
		{"sql_group", render(map[string]string{"language_name": "sql", "grantee_type": "GROUP", "grantee": "udf_devs"})},
		{"plpgsql_public", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"})},
		{"canonical_language_case", render(map[string]string{"language_name": "SQL"})},
		{"quoted_identifiers", render(map[string]string{"database_name": `Odd"Database`, "grantee_type": "USER", "grantee": `Odd"O'Reilly\User`})},
		{"python_rejected", render(map[string]string{"language_name": "plpythonu"})},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"})},
		{"empty_database_name", render(map[string]string{"database_name": ""})},
	})
}
