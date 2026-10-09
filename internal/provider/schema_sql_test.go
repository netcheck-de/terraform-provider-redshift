package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestSchemaSQL pins the schema statements and lookup, including names that need quoting.
func TestSchemaSQL(t *testing.T) {
	schema := func(name string) schemaModel {
		return schemaModel{Database: types.StringValue("analytics"), Name: types.StringValue(name)}
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "schema", []sqlCase{
		{"create", func() string { return createSchemaStatement(schema("serving")) }},
		{"create_quoted", func() string { return createSchemaStatement(schema(`Odd"Serving`)) }},
		{"drop", func() string { return dropSchemaStatement(schema("serving")) }},
		{"drop_quoted", func() string { return dropSchemaStatement(schema(`Odd"Serving`)) }},
		{"read", built(readSchemaQuery(schema(`Odd"Serving`)))},
		{"read_empty_name", built(readSchemaQuery(schema("")))},
	})
}
