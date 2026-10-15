package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestGroupMembershipSQL pins adding and removing one member and the membership lookup.
func TestGroupMembershipSQL(t *testing.T) {
	membership := func(group, user string) groupMembershipModel {
		return groupMembershipModel{Group: types.StringValue(group), User: types.StringValue(user)}
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "group_membership", []sqlCase{
		{"create", func() string { return createGroupMembershipStatement(membership("readers", "grafana")) }},
		{"create_quoted", func() string { return createGroupMembershipStatement(membership(`Odd"Readers`, `Odd"User`)) }},
		{"drop", func() string { return dropGroupMembershipStatement(membership("readers", "grafana")) }},
		{"drop_quoted", func() string { return dropGroupMembershipStatement(membership(`Odd"Readers`, `Odd"User`)) }},
		{"read", built(readGroupMembershipQuery(membership(`Odd"Readers`, `Odd"User`)))},
		{"read_empty_user", built(readGroupMembershipQuery(membership("readers", "")))},
	})
}
