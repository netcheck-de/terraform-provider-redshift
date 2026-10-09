package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createRoleStatement renders CREATE ROLE for the configured name.
func createRoleStatement(data roleModel) string {
	return sqlclient.Stmt("CREATE ROLE").Ident(data.Name.ValueString()).String()
}

// dropRoleStatement renders DROP ROLE without FORCE, so a role that is still granted to others is kept.
func dropRoleStatement(data roleModel) string {
	return sqlclient.Stmt("DROP ROLE").Ident(data.Name.ValueString()).String()
}

// readRoleQuery looks up the role by its exact catalog name.
func readRoleQuery(data roleModel) sqlclient.Query {
	return sqlclient.Select("role_name").From("svv_roles").Where("role_name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
