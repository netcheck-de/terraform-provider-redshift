package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// roleAlter starts every ALTER ROLE statement for the role.
func roleAlter(data roleModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER ROLE").Ident(data.Name.ValueString())
}

// createRoleStatement renders CREATE ROLE with the optional EXTERNALID. r_CREATE_ROLE spells the external ID as a
// double-quoted identifier (EXTERNALID "ABC123"), not as a string literal.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_ROLE.html
func createRoleStatement(data roleModel) string {
	return sqlclient.Stmt("CREATE ROLE").Ident(data.Name.ValueString()).OptIdent("EXTERNALID", knownString(data.ExternalID)).String()
}

// createRoleStatements renders the creation and, because CREATE ROLE has no OWNER clause, the ownership transfer of
// a configured owner. An unset owner keeps the creating user.
func createRoleStatements(data roleModel) []string {
	statements := []string{createRoleStatement(data)}
	if owner := knownString(data.Owner); owner != "" {
		statements = append(statements, roleAlter(data).KwIdent("OWNER TO", owner).String())
	}
	return statements
}

// roleAlterSteps change one ALTER ROLE option per statement (r_ALTER_ROLE). Both attributes are Optional+Computed
// and keep their state when unset, so a null plan never asks to remove an owner or external ID, which ALTER ROLE
// cannot express.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_ROLE.html
var roleAlterSteps = []alterStep[roleModel]{
	{
		attribute: "owner",
		value:     func(data roleModel) attr.Value { return data.Owner },
		render: func(_, plan roleModel) []string {
			if owner := knownString(plan.Owner); owner != "" {
				return []string{roleAlter(plan).KwIdent("OWNER TO", owner).String()}
			}
			return nil
		},
	},
	{
		attribute: "external_id",
		value:     func(data roleModel) attr.Value { return data.ExternalID },
		render: func(_, plan roleModel) []string {
			if external := knownString(plan.ExternalID); external != "" {
				return []string{roleAlter(plan).KwIdent("EXTERNALID TO", external).String()}
			}
			return nil
		},
	},
}

// alterRoleStatements renders the in-place changes from prev to plan.
func alterRoleStatements(prev, plan roleModel) []string {
	return alterStatements(prev, plan, roleAlterSteps)
}

// dropRoleStatement renders DROP ROLE without FORCE, so a role that is still granted to others is kept.
func dropRoleStatement(data roleModel) string {
	return sqlclient.Stmt("DROP ROLE").Ident(data.Name.ValueString()).String()
}

// readRoleQuery looks up the role by its exact catalog name, with the owner and external ID that svv_roles reports.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_ROLES.html
func readRoleQuery(data roleModel) sqlclient.Query {
	return sqlclient.Select("role_id", "role_name", "role_owner", "external_id").From("svv_roles").
		Where("role_name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
