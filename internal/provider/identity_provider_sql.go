package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// identityProviderAlter starts every ALTER IDENTITY PROVIDER statement for the provider.
func identityProviderAlter(data identityProviderModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER IDENTITY PROVIDER").Ident(data.Name.ValueString())
}

// createIdentityProviderStatement renders CREATE IDENTITY PROVIDER for an AWSIDC integration. A new provider
// is enabled; identityProviderStatusStatement disables it separately because CREATE has no such option.
func createIdentityProviderStatement(data identityProviderModel) (string, error) {
	statement := sqlclient.Stmt("CREATE IDENTITY PROVIDER").Ident(data.Name.ValueString()).Kw("TYPE AWSIDC").
		KwLit("NAMESPACE", data.Namespace.ValueString()).
		KwLit("APPLICATION_ARN", data.ApplicationARN.ValueString()).
		KwLit("IAM_ROLE", data.IAMRoleARN.ValueString())
	return statement.String(), statement.Err()
}

// identityProviderStatusStatement renders ENABLE or DISABLE.
func identityProviderStatusStatement(data identityProviderModel) string {
	return identityProviderAlter(data).Toggle(data.Enabled.ValueBool(), "ENABLE", "DISABLE").String()
}

// identityProviderRoleStatement renders the IAM_ROLE change.
func identityProviderRoleStatement(data identityProviderModel) string {
	return identityProviderAlter(data).KwLit("IAM_ROLE", data.IAMRoleARN.ValueString()).String()
}

// alterIdentityProviderStatements renders the role and status of plan. Both are reapplied on every update,
// whatever the prior state, so an update also repairs drift that a refresh did not observe yet.
func alterIdentityProviderStatements(plan identityProviderModel) []string {
	return []string{identityProviderRoleStatement(plan), identityProviderStatusStatement(plan)}
}

// dropIdentityProviderStatement renders DROP IDENTITY PROVIDER.
func dropIdentityProviderStatement(data identityProviderModel) string {
	return sqlclient.Stmt("DROP IDENTITY PROVIDER").Ident(data.Name.ValueString()).String()
}

// readIdentityProviderQuery selects the catalog row of one identity provider.
func readIdentityProviderQuery(data identityProviderModel) sqlclient.Query {
	return sqlclient.Select("name", "type", "instanceid", "namespc", "params", "enabled").From("svv_identity_providers").
		Where("name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// readIdentityProviderUsersQuery finds federated users carrying the provider's namespace prefix. LEFT avoids
// LIKE, whose _ and % wildcards a namespace may contain.
func readIdentityProviderUsersQuery(data identityProviderModel) sqlclient.Query {
	return sqlclient.Select("usename").From("pg_user").
		Where("LEFT(usename, LENGTH(:prefix)) = :prefix", sqlclient.Bind("prefix", data.Namespace.ValueString()+":"))
}
