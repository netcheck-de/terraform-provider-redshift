package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// identityProviderAWSIDCFixture builds an AWSIDC provider; unset Azure and role-creation attributes stay null.
func identityProviderAWSIDCFixture(name, namespace, role string, enabled bool) identityProviderModel {
	return identityProviderModel{
		Name: types.StringValue(name), Type: types.StringValue(identityProviderAWSIDC), Namespace: types.StringValue(namespace),
		ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue(role), Enabled: types.BoolValue(enabled),
	}
}

// identityProviderAzureFixture builds an Azure provider with the r_CREATE_IDENTITY_PROVIDER example parameters.
func identityProviderAzureFixture(name, namespace, issuer string) identityProviderModel {
	return identityProviderModel{
		Name: types.StringValue(name), Type: types.StringValue(identityProviderAzure), Namespace: types.StringValue(namespace),
		Issuer: types.StringValue(issuer), ClientID: types.StringValue("87f4aa26-78b7-410e-bf29-57b39929ef9a"),
		Audience:            []string{"https://analysis.windows.net/powerbi/connector/AmazonRedshift"},
		ClientSecretVersion: types.Int64Value(1), Enabled: types.BoolValue(true),
	}
}

// TestIdentityProviderSQL pins every identity provider statement against r_CREATE_IDENTITY_PROVIDER,
// r_ALTER_IDENTITY_PROVIDER, and r_DROP_IDENTITY_PROVIDER, including quoting of names, options, and JSON parameters.
func TestIdentityProviderSQL(t *testing.T) {
	plain := identityProviderAWSIDCFixture("identity", "example", "role-one", true)
	quoted := identityProviderAWSIDCFixture(`Odd"IdP`, `it's \ns`, `role-'two\`, false)
	azure := identityProviderAzureFixture("oauth_standard", "aad", "https://sts.windows.net/2sdfdsf-d475-420d-b5ac-667adad7c702/")
	azureQuoted := identityProviderAzureFixture(`Odd"Azure`, `it's \aad`, `https://issuer.example/it's\"x"`)
	azureQuoted.Audience = []string{"https://b.example/'quoted'", `https://a.example/back\slash`}
	azureQuoted.AutoCreateRoles, azureQuoted.ExcludeGroups = types.BoolValue(true), types.StringValue("%_admins$")
	const secret = `BUAH~ewrqewrqwerUUY^%tHe1oNZShoiU7`
	const quotedSecret = `it's "a" \secret`
	with := func(model identityProviderModel, change func(*identityProviderModel)) identityProviderModel {
		change(&model)
		return model
	}
	autoCreate := with(plain, func(m *identityProviderModel) {
		m.AutoCreateRoles, m.IncludeGroups = types.BoolValue(true), types.StringValue("finance_%")
	})
	create := func(data identityProviderModel, secret string) func() (string, error) {
		return func() (string, error) { return createIdentityProviderStatement(data, secret) }
	}
	alter := func(prev, plan identityProviderModel, secret string) func() ([]string, error) {
		return func() ([]string, error) { return alterIdentityProviderStatements(prev, plan, secret) }
	}
	checkSQL(t, "identity_provider", []sqlCase{
		{"create", create(plain, "")},
		{"create_quoted", create(quoted, "")},
		{"create_auto_create_roles_include", create(autoCreate, "")},
		{"create_auto_create_roles_false", create(with(plain, func(m *identityProviderModel) { m.AutoCreateRoles = types.BoolValue(false) }), "")},
		{"create_azure", create(azure, secret)},
		{"create_azure_quoted", create(azureQuoted, quotedSecret)},
		{"create_azure_without_secret", create(azure, "")},
		{"create_azure_with_application_arn", create(with(azure, func(m *identityProviderModel) { m.ApplicationARN = types.StringValue("application") }), secret)},
		{"create_azure_without_issuer", create(with(azure, func(m *identityProviderModel) { m.Issuer = types.StringNull() }), secret)},
		{"create_awsidc_with_audience", create(with(plain, func(m *identityProviderModel) { m.Audience = []string{"aud"} }), "")},
		{"create_awsidc_with_secret", create(plain, secret)},
		{"create_awsidc_with_secret_version", create(with(plain, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Value(1) }), "")},
		{"create_azure_without_secret_version", create(with(azure, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Null() }), secret)},
		{"create_awsidc_without_role", create(with(plain, func(m *identityProviderModel) { m.IAMRoleARN = types.StringNull() }), "")},
		{"create_filter_without_auto_create", create(with(plain, func(m *identityProviderModel) { m.IncludeGroups = types.StringValue("x") }), "")},
		{"create_both_filters", create(with(autoCreate, func(m *identityProviderModel) { m.ExcludeGroups = types.StringValue("y") }), "")},
		{"create_unsupported_type", create(with(plain, func(m *identityProviderModel) { m.Type = types.StringValue("okta") }), "")},
		{"status_enabled", func() string { return identityProviderStatusStatement(plain) }},
		{"status_disabled", func() string {
			return identityProviderStatusStatement(identityProviderAWSIDCFixture("identity", "example", "role-one", false))
		}},
		{"alter_enabled", alter(plain, plain, "")},
		{"alter_disabled_quoted", alter(quoted, quoted, "")},
		{"alter_namespace_quoted", alter(plain, with(plain, func(m *identityProviderModel) { m.Namespace = types.StringValue(`it's \ns`) }), "")},
		{"alter_auto_create_roles", alter(plain, autoCreate, "")},
		{"alter_auto_create_roles_reset_awsidc", alter(autoCreate, plain, "")},
		{"alter_auto_create_roles_reset_azure", alter(with(azure, func(m *identityProviderModel) { m.AutoCreateRoles = types.BoolValue(false) }), azure, "")},
		{"alter_auto_create_roles_filter", alter(autoCreate, with(autoCreate, func(m *identityProviderModel) {
			m.IncludeGroups, m.ExcludeGroups = types.StringNull(), types.StringValue("it's")
		}), "")},
		{"alter_awsidc_with_secret", alter(plain, plain, secret)},
		{"alter_azure_unchanged", alter(azure, azure, "")},
		{"alter_azure_issuer", alter(azure, with(azure, func(m *identityProviderModel) {
			m.Issuer = types.StringValue("https://login.microsoftonline.com/tenant/v2.0")
		}), secret)},
		{"alter_azure_audience_quoted", alter(azure, azureQuoted, quotedSecret)},
		{"alter_azure_rotate_secret", alter(azure, with(azure, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Value(2) }), secret)},
		{"alter_azure_imported_version", alter(with(azure, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Null() }), azure, "")},
		{"alter_azure_without_secret", alter(azure, with(azure, func(m *identityProviderModel) { m.ClientID = types.StringValue("other") }), "")},
		{"alter_azure_all", alter(azure, with(azure, func(m *identityProviderModel) {
			m.Namespace, m.ClientID, m.AutoCreateRoles, m.Enabled = types.StringValue("entra"), types.StringValue("other"), types.BoolValue(false), types.BoolValue(false)
		}), secret)},
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
