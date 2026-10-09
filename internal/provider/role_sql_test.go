package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestRoleSQL pins the role statements and lookup, including names that need quoting.
func TestRoleSQL(t *testing.T) {
	role := func(name string) roleModel { return roleModel{Name: types.StringValue(name)} }
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "role", []sqlCase{
		{"create", func() string { return createRoleStatement(role("readers")) }},
		{"create_namespaced", func() string { return createRoleStatement(role("example:readers")) }},
		{"create_quoted", func() string { return createRoleStatement(role(`Odd"Readers`)) }},
		{"drop", func() string { return dropRoleStatement(role("readers")) }},
		{"drop_quoted", func() string { return dropRoleStatement(role(`Odd"Readers`)) }},
		{"read", built(readRoleQuery(role(`Odd"Readers`)))},
		{"read_empty_name", built(readRoleQuery(role("")))},
	})
}
