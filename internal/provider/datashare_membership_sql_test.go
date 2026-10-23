package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestDatashareMembershipSQL pins the superuser check, ADD and REMOVE statements, and member queries, including quoted
// names and the dotted-schema refusal.
func TestDatashareMembershipSQL(t *testing.T) {
	query := func(q sqlclient.Query) func() (string, error) {
		return func() (string, error) { sql, _, err := q.Build(); return sql, err }
	}
	tables, functions := datashareMembershipFixture("TABLES"), datashareMembershipFixture("FUNCTIONS")
	quoted := tables
	quoted.Datashare, quoted.Schema = types.StringValue(`Odd"Share`), types.StringValue(`Odd"Schema`)
	invalid := tables
	invalid.Schema = types.StringValue("ambiguous.schema")
	checkSQL(t, "datashare_membership", []sqlCase{
		{"superuser", query(readDatashareMembershipSuperuserQuery())},
		{"add_tables", func() (string, error) { return createDatashareMembershipStatement(tables) }},
		{"add_functions", func() (string, error) { return createDatashareMembershipStatement(functions) }},
		{"remove_tables", func() (string, error) { return dropDatashareMembershipStatement(tables) }},
		{"remove_functions", func() (string, error) { return dropDatashareMembershipStatement(functions) }},
		{"quoted", func() (string, error) { return createDatashareMembershipStatement(quoted) }},
		{"ambiguous_schema", func() (string, error) { return createDatashareMembershipStatement(invalid) }},
		{"table_candidates", query(readDatashareMembershipCandidatesQuery(tables))},
		{"function_candidates", query(readDatashareMembershipCandidatesQuery(functions))},
		{"table_members", query(readDatashareMembershipObjectsQuery(tables))},
		{"function_members", query(readDatashareMembershipObjectsQuery(functions))},
	})
}
