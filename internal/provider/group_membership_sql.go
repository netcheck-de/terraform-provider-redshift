package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createGroupMembershipStatement adds only this user, leaving the group's other members alone.
func createGroupMembershipStatement(data groupMembershipModel) string {
	return sqlclient.Stmt("ALTER GROUP").Ident(data.Group.ValueString()).KwIdent("ADD USER", data.User.ValueString()).String()
}

// dropGroupMembershipStatement removes only this user, leaving the group's other members alone.
func dropGroupMembershipStatement(data groupMembershipModel) string {
	return sqlclient.Stmt("ALTER GROUP").Ident(data.Group.ValueString()).KwIdent("DROP USER", data.User.ValueString()).String()
}

// groupMembershipSource joins each group to its member users, so a row exists only for an explicit membership.
const groupMembershipSource = "pg_group g JOIN pg_user u ON u.usesysid = ANY(g.grolist)"

// readGroupMembershipQuery returns a row only while the user is a member of the group.
func readGroupMembershipQuery(data groupMembershipModel) sqlclient.Query {
	return sqlclient.Select("g.groname").From(groupMembershipSource).
		Where("g.groname = :group", sqlclient.Bind("group", data.Group.ValueString())).
		Where("u.usename = :user", sqlclient.Bind("user", data.User.ValueString()))
}

// listGroupMembershipsQuery returns every membership, optionally of one group or one user.
func listGroupMembershipsQuery(group, user string) sqlclient.Query {
	return sqlclient.Select("g.groname", "u.usename").From(groupMembershipSource).
		OptEq("g.groname", "group", group).OptEq("u.usename", "user", user).OrderBy("g.groname", "u.usename")
}
