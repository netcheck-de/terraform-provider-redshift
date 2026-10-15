package provider

import (
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// errRoleGrantAdminOption rejects WITH ADMIN OPTION for a role recipient: r_GRANT documents it only after a user name,
// and svv_role_grants has no admin_option column to verify it.
var errRoleGrantAdminOption = errors.New("admin_option requires to_user; Redshift grants WITH ADMIN OPTION only to users")

// adminOption reports whether the grant should carry WITH ADMIN OPTION; null, as in state written before the
// attribute existed, means no.
func (data roleGrantModel) adminOption() bool {
	return knownBool(data.AdminOption) != nil && data.AdminOption.ValueBool()
}

// validate rejects tuples Redshift cannot grant, before any SQL runs.
func (data roleGrantModel) validate() error {
	if data.adminOption() && data.ToUser.IsNull() {
		return errRoleGrantAdminOption
	}
	return nil
}

// spec renders GRANT ROLE "role" TO recipient through the shared grant shape, with ROLE as the privilege keyword.
// WITH ADMIN OPTION follows a user recipient on GRANT only (r_GRANT, "Granting role permissions").
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles
func (data roleGrantModel) spec() grantSpec {
	spec := grantSpec{object: sqlclient.Ident(data.Role.ValueString()), grantee: data.recipient()}
	if data.adminOption() {
		spec.option = "WITH ADMIN OPTION"
	}
	return spec
}

// recipient renders the receiving user or role without conflating them; a bare name is a user.
func (data roleGrantModel) recipient() sqlclient.Statement {
	if !data.ToUser.IsNull() {
		return sqlclient.Ident(data.ToUser.ValueString())
	}
	return sqlclient.Kw("ROLE").Ident(data.ToRole.ValueString())
}

// createRoleGrantStatement grants the role to the recipient, with the admin option when configured.
func createRoleGrantStatement(data roleGrantModel) (string, error) {
	if err := data.validate(); err != nil {
		return "", err
	}
	return data.spec().statement(true, "ROLE"), nil
}

// dropRoleGrantStatement revokes only this role from the recipient, which also removes its admin option.
func dropRoleGrantStatement(data roleGrantModel) string {
	return data.spec().statement(false, "ROLE")
}

// revokeRoleGrantAdminStatement keeps the membership and removes only the right to grant the role to others, with
// REVOKE ADMIN OPTION FOR, which r_REVOKE documents for user recipients.
// https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html#revoke-roles
func revokeRoleGrantAdminStatement(data roleGrantModel) string {
	return sqlclient.Stmt("REVOKE ADMIN OPTION FOR").Kw("ROLE").Ident(data.Role.ValueString()).Kw("FROM").Append(data.recipient()).String()
}

// roleGrantAlterSteps change the admin option in place: granting the role again WITH ADMIN OPTION upgrades the
// membership, and REVOKE ADMIN OPTION FOR downgrades it without revoking the role.
var roleGrantAlterSteps = []alterStep[roleGrantModel]{
	{
		attribute: "admin_option",
		value:     func(data roleGrantModel) attr.Value { return types.BoolValue(data.adminOption()) },
		render: func(_, plan roleGrantModel) []string {
			if plan.adminOption() {
				return []string{plan.spec().statement(true, "ROLE")}
			}
			return []string{revokeRoleGrantAdminStatement(plan)}
		},
	},
}

// alterRoleGrantStatements renders the in-place changes from prev to plan.
func alterRoleGrantStatements(prev, plan roleGrantModel) ([]string, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, roleGrantAlterSteps), nil
}

// readRoleGrantQuery checks explicit membership; users and roles are recorded in different catalog views, and only
// svv_user_grants reports admin_option.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_USER_GRANTS.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_ROLE_GRANTS.html
func readRoleGrantQuery(data roleGrantModel) sqlclient.Query {
	role := sqlclient.Bind("role", data.Role.ValueString())
	if !data.ToUser.IsNull() {
		return sqlclient.Select("role_name", "admin_option").From("svv_user_grants").Where("user_name = :user", sqlclient.Bind("user", data.ToUser.ValueString())).Where("role_name = :role", role)
	}
	return sqlclient.Select("role_name").From("svv_role_grants").Where("role_name = :recipient", sqlclient.Bind("recipient", data.ToRole.ValueString())).Where("granted_role_name = :role", role)
}
