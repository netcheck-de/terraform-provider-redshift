package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserSQL pins every user statement, including quoting of identifiers and secrets.
func TestUserSQL(t *testing.T) {
	quoted := grafanaUser()
	quoted.Name = types.StringValue(`Odd"User`)
	with := func(base userModel, change func(*userModel)) userModel {
		change(&base)
		return base
	}
	create := func(data userModel, secret string) func() (string, error) {
		return func() (string, error) { return createUserStatement(data, secret) }
	}
	alter := func(prev, plan userModel) func() ([]string, error) {
		return func() ([]string, error) { return alterUserStatements(prev, plan) }
	}
	rotated := func(data *userModel) {
		data.PasswordVersion, data.Password = types.Int64Value(1), types.StringValue(`it's \new`)
	}
	privileged := func(data *userModel) { data.Superuser, data.CreateDB = types.BoolValue(true), types.BoolValue(true) }
	checkSQL(t, "user", []sqlCase{
		{"create", create(grafanaUser(), "InitialPass123")},
		{"create_superuser_createdb", create(with(grafanaUser(), privileged), "InitialPass123")},
		{"create_createdb_only", create(with(grafanaUser(), func(data *userModel) { data.CreateDB = types.BoolValue(true) }), "InitialPass123")},
		{"create_quoted", create(quoted, `it's \secret`)},
		{"alter_unchanged", alter(grafanaUser(), grafanaUser())},
		{"alter_rotate_password", alter(quoted, with(quoted, rotated))},
		{"alter_rotate_without_password", alter(grafanaUser(), with(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_rotate_from_null_version", alter(with(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Null() }), with(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_password_unchanged_version", alter(grafanaUser(), with(grafanaUser(), func(data *userModel) { data.Password = types.StringValue("ignored") }))},
		{"alter_grant_capabilities", alter(grafanaUser(), with(grafanaUser(), privileged))},
		{"alter_revoke_capabilities", alter(with(quoted, privileged), quoted)},
		{"alter_capabilities_null_prior", alter(with(grafanaUser(), func(data *userModel) { data.Superuser, data.CreateDB = types.BoolNull(), types.BoolNull() }), with(grafanaUser(), privileged))},
		{"alter_all", alter(grafanaUser(), with(with(grafanaUser(), privileged), rotated))},
		{"drop", func() string { return dropUserStatement(grafanaUser()) }},
		{"drop_quoted", func() string { return dropUserStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readUserQuery(quoted).Build()
			assert.Equal(t, map[string]string{"name": `Odd"User`}, parameters)
			return sql, err
		}},
	})
}

// TestUserAlterCoverage keeps an update step for every in-place user attribute; the secret only feeds rotation.
func TestUserAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newUserResource(), userAlterSteps, "password_wo")
}

// TestUserReadQueryRejectsEmptyName fails before sending a binding the Data API would reject.
func TestUserReadQueryRejectsEmptyName(t *testing.T) {
	_, _, err := readUserQuery(userModel{Name: types.StringValue("")}).Build()
	require.ErrorContains(t, err, ":name is empty")
}
