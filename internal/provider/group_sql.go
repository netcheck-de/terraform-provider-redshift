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

// readGroupQuery looks up the group by its exact catalog name with one row per member, joined the way the AWS
// groups page lists users by group. The left join keeps one row with an empty user name for a group without members.
func readGroupQuery(data groupModel) sqlclient.Query {
	return sqlclient.Select("g.groname", "g.grosysid", "u.usename").
		From("pg_group g LEFT JOIN pg_user u ON u.usesysid = ANY(g.grolist)").
		Where("g.groname = :name", sqlclient.Bind("name", data.Name.ValueString())).
		OrderBy("u.usename")
}
