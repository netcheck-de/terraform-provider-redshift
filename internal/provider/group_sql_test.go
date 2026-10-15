package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestGroupSQL pins the group statements and lookup, including names that need quoting.
func TestGroupSQL(t *testing.T) {
	group := func(name string) groupModel { return groupModel{Name: types.StringValue(name)} }
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "group", []sqlCase{
		{"create", func() string { return createGroupStatement(group("readers")) }},
		{"create_quoted", func() string { return createGroupStatement(group(`Odd"Readers`)) }},
		{"drop", func() string { return dropGroupStatement(group("readers")) }},
		{"drop_quoted", func() string { return dropGroupStatement(group(`Odd"Readers`)) }},
		{"read", built(readGroupQuery(group(`Odd"Readers`)))},
		{"read_empty_name", built(readGroupQuery(group("")))},
	})
}
