package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// privilegeTargetSQL renders a target's existence checks and privilege read, followed by one GRANT and one
// REVOKE of privilege, so a golden file shows every statement the tuple can send.
func privilegeTargetSQL(target privilegeTarget, err error, privilege sqlclient.Keyword) ([]string, error) {
	if err != nil {
		return nil, err
	}
	var statements []string
	for _, check := range target.checks {
		if check.sql != "" {
			statements = append(statements, check.sql)
		}
	}
	return append(statements, target.query.sql, target.grant.statement(true, privilege), target.grant.statement(false, privilege)), nil
}

// privilegeTargetCase renders the target that factory's prepare builds from fields.
func privilegeTargetCase(t *testing.T, factory func() resource.Resource, fields map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
	t.Helper()
	return func() ([]string, error) {
		r := factory().(*privilegeResource)
		target, err := r.prepare(privilegeObject(t, r, fields))
		return privilegeTargetSQL(target, err, privilege)
	}
}

// TestGrantSpecSQL pins the shared GRANT/REVOKE shape and its optional parts; the assumerole_grant goldens
// cover the render override.
func TestGrantSpecSQL(t *testing.T) {
	both := func(spec grantSpec, privilege sqlclient.Keyword) func() []string {
		return func() []string { return []string{spec.statement(true, privilege), spec.statement(false, privilege)} }
	}
	object := sqlclient.Fragment().KwQualified("ON TABLE", `Odd"Database`, "serving", `Odd"Table`)
	checkSQL(t, "privilege_sql/grant_spec", []sqlCase{
		{"object_role", both(grantSpec{object: object, grantee: sqlclient.Fragment().KwIdent("ROLE", `Odd"Role`)}, "SELECT")},
		{"without_object", both(grantSpec{grantee: sqlclient.Fragment().KwIdent("ROLE", "operators")}, "CREATE USER")},
		{"prefix", both(grantSpec{prefix: sqlclient.Stmt("ALTER DEFAULT PRIVILEGES FOR USER").Ident("loader"), object: sqlclient.Kw("ON", "TABLES"), grantee: sqlclient.Kw("PUBLIC")}, "INSERT")},
		{"option", both(grantSpec{object: object, grantee: sqlclient.Ident("analyst"), option: "WITH GRANT OPTION"}, "SELECT")},
	})
}

// TestPrivilegeStatementsSQL pins revoke-before-grant ordering, sorted input, and the render-time allowlist.
func TestPrivilegeStatementsSQL(t *testing.T) {
	spec := grantSpec{object: sqlclient.Fragment().KwIdent("ON DATABASE", "analytics"), grantee: sqlclient.Fragment().KwIdent("GROUP", "readers")}
	allowed := []sqlclient.Keyword{"CREATE", "TEMPORARY", "USAGE"}
	render := func(current, desired []string) func() ([]string, error) {
		return func() ([]string, error) { return privilegeStatements(spec, allowed, current, desired) }
	}
	checkSQL(t, "privilege_sql/statements", []sqlCase{
		{"converged", render([]string{"CREATE"}, []string{"CREATE"})},
		{"grant_missing", render([]string{"CREATE"}, []string{"CREATE", "TEMPORARY", "USAGE"})},
		{"revoke_before_grant", render([]string{"CREATE", "TEMPORARY"}, []string{"USAGE"})},
		{"revoke_all", render([]string{"CREATE", "USAGE"}, nil)},
		{"case_insensitive_allowlist", render(nil, []string{"usage"})},
		{"unsupported_privilege", render(nil, []string{"USAGE; DROP TABLE x"})},
	})
}

// TestPrincipalSQL pins every grantee form, its existence check, and quoting of names with quotes and case.
func TestPrincipalSQL(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	render := func(kind, name string) func() ([]string, error) {
		return func() ([]string, error) {
			grantee, check, err := principal(privilegeObject(t, r, map[string]string{"grantee_type": kind, "grantee": name}))
			if err != nil {
				return nil, err
			}
			statements := []string{sqlclient.Stmt("GRANT USAGE ON SCHEMA").Ident("serving").Kw("TO").Append(grantee).String()}
			if check.sql != "" {
				statements = append([]string{check.sql}, statements...)
			}
			return statements, nil
		}
	}
	checkSQL(t, "privilege_sql/principal", []sqlCase{
		{"role", render("ROLE", "readers")},
		{"user", render("USER", "analyst")},
		{"group", render("GROUP", "readers")},
		{"public", render("PUBLIC", "public")},
		{"quoted_role", render("ROLE", `Odd"Role`)},
		{"quoted_user", render("USER", `Odd"User`)},
		{"public_other_name", render("PUBLIC", "readers")},
		{"unsupported_kind", render("DATASHARE", "producer")},
		{"empty_name", render("ROLE", "")},
	})
}

// TestNewCatalogCheck keeps bindings in sync with the built query and rejects empty values.
func TestNewCatalogCheck(t *testing.T) {
	check, err := newCatalogCheck(privilegeSchemaQuery("analytics", "serving"))
	require.NoError(t, err)
	assert.Equal(t, catalogCheck{
		sql:        "SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema",
		parameters: map[string]string{"database": "analytics", "schema": "serving"},
	}, check)
	_, err = newCatalogChecks(privilegeUserQuery("loader"), privilegeUserQuery(""))
	require.ErrorContains(t, err, "query parameter :name is empty")
	assert.Equal(t, []string{"CREATE", "USAGE"}, privilegeNames([]sqlclient.Keyword{"CREATE", "USAGE"}))
	assert.True(t, privilegeAllowed([]sqlclient.Keyword{"USAGE"}, "USAGE"))
	assert.False(t, privilegeAllowed([]sqlclient.Keyword{"USAGE"}, "usage"), "validation stays case-sensitive")
}
