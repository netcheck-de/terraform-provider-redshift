package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestIdentityProviderSQL pins every identity provider statement, including quoting of names and options.
func TestIdentityProviderSQL(t *testing.T) {
	model := func(name, namespace, role string, enabled bool) identityProviderModel {
		return identityProviderModel{
			Name: types.StringValue(name), Namespace: types.StringValue(namespace), ApplicationARN: types.StringValue("application"),
			IAMRoleARN: types.StringValue(role), Enabled: types.BoolValue(enabled),
		}
	}
	plain := model("identity", "example", "role-one", true)
	quoted := model(`Odd"IdP`, `it's \ns`, `role-'two\`, false)
	create := func(data identityProviderModel) func() (string, error) {
		return func() (string, error) { return createIdentityProviderStatement(data) }
	}
	checkSQL(t, "identity_provider", []sqlCase{
		{"create", create(plain)},
		{"create_quoted", create(quoted)},
		{"status_enabled", func() string { return identityProviderStatusStatement(plain) }},
		{"status_disabled", func() string { return identityProviderStatusStatement(model("identity", "example", "role-one", false)) }},
		{"alter_enabled", func() []string { return alterIdentityProviderStatements(plain) }},
		{"alter_disabled_quoted", func() []string { return alterIdentityProviderStatements(quoted) }},
		{"drop", func() string { return dropIdentityProviderStatement(plain) }},
		{"drop_quoted", func() string { return dropIdentityProviderStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readIdentityProviderQuery(quoted).Build()
			assert.Equal(t, map[string]string{"name": `Odd"IdP`}, parameters)
			return sql, err
		}},
		{"read_federated_users", func() (string, error) {
			sql, parameters, err := readIdentityProviderUsersQuery(quoted).Build()
			assert.Equal(t, map[string]string{"prefix": `it's \ns:`}, parameters)
			return sql, err
		}},
	})
}
