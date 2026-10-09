package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestDatashareSchemaSQL pins adding a schema with and without future objects, toggling include_new, removal and
// the member lookup.
func TestDatashareSchemaSQL(t *testing.T) {
	member := func(share, schema string, includeNew bool) datashareSchemaModel {
		return datashareSchemaModel{Database: types.StringValue("analytics"), Datashare: types.StringValue(share), Schema: types.StringValue(schema), IncludeNew: types.BoolValue(includeNew)}
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "datashare_schema", []sqlCase{
		{"create", func() []string { return createDatashareSchemaStatements(member("producer", "serving", false)) }},
		{"create_include_new", func() []string { return createDatashareSchemaStatements(member("producer", "serving", true)) }},
		{"create_quoted", func() []string { return createDatashareSchemaStatements(member(`Odd"Producer`, `Odd"Serving`, true)) }},
		{"alter", func() string { return alterDatashareSchemaStatement(member("producer", "serving", false)) }},
		{"alter_include_new", func() string { return alterDatashareSchemaStatement(member("producer", "serving", true)) }},
		{"alter_quoted", func() string { return alterDatashareSchemaStatement(member(`Odd"Producer`, `Odd"Serving`, true)) }},
		{"drop", func() string { return dropDatashareSchemaStatement(member("producer", "serving", false)) }},
		{"drop_quoted", func() string { return dropDatashareSchemaStatement(member(`Odd"Producer`, `Odd"Serving`, false)) }},
		{"read", built(readDatashareSchemaQuery(member(`Odd"Producer`, `Odd"Serving`, false)))},
		{"read_empty_schema", built(readDatashareSchemaQuery(member("producer", "", false)))},
	})
}
