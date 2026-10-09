package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createGroupStatement renders CREATE GROUP without members, because group_membership owns them.
func createGroupStatement(data groupModel) string {
	return sqlclient.Stmt("CREATE GROUP").Ident(data.Name.ValueString()).String()
}

// dropGroupStatement renders DROP GROUP for the configured name.
func dropGroupStatement(data groupModel) string {
	return sqlclient.Stmt("DROP GROUP").Ident(data.Name.ValueString()).String()
}

// readGroupQuery looks up the group by its exact catalog name.
func readGroupQuery(data groupModel) sqlclient.Query {
	return sqlclient.Select("groname").From("pg_group").Where("groname = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
