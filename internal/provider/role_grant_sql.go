package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// spec renders GRANT ROLE "role" TO recipient through the shared grant shape, with ROLE as the privilege keyword.
func (data roleGrantModel) spec() grantSpec {
	return grantSpec{object: sqlclient.Ident(data.Role.ValueString()), grantee: data.recipient()}
}

// recipient renders the receiving user or role without conflating them; a bare name is a user.
func (data roleGrantModel) recipient() sqlclient.Statement {
	if !data.ToUser.IsNull() {
		return sqlclient.Ident(data.ToUser.ValueString())
	}
	return sqlclient.Kw("ROLE").Ident(data.ToRole.ValueString())
}

// createRoleGrantStatement grants the role to the recipient.
func createRoleGrantStatement(data roleGrantModel) string {
	return data.spec().statement(true, "ROLE")
}

// dropRoleGrantStatement revokes only this role from the recipient.
func dropRoleGrantStatement(data roleGrantModel) string {
	return data.spec().statement(false, "ROLE")
}

// readRoleGrantQuery checks explicit membership; users and roles are recorded in different catalog views.
func readRoleGrantQuery(data roleGrantModel) sqlclient.Query {
	role := sqlclient.Bind("role", data.Role.ValueString())
	if !data.ToUser.IsNull() {
		return sqlclient.Select("role_name").From("svv_user_grants").Where("user_name = :user", sqlclient.Bind("user", data.ToUser.ValueString())).Where("role_name = :role", role)
	}
	return sqlclient.Select("role_name").From("svv_role_grants").Where("role_name = :recipient", sqlclient.Bind("recipient", data.ToRole.ValueString())).Where("granted_role_name = :role", role)
}
