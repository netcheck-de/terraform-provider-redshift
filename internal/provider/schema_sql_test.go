package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// TestSchemaSQL pins the schema statements and lookups, including names that need quoting.
func TestSchemaSQL(t *testing.T) {
	schema := func(name string) schemaModel {
		return schemaModel{Database: types.StringValue("analytics"), Name: types.StringValue(name)}
	}
	options := func(name, owner string, quota int64) schemaModel {
		data := schema(name)
		data.Owner, data.Quota = types.StringValue(owner), types.Int64Value(quota)
		return data
	}
	create := func(data schemaModel) func() (string, error) {
		return func() (string, error) { return createSchemaStatement(data) }
	}
	alter := func(prev, plan schemaModel) func() ([]string, error) {
		return func() ([]string, error) { return alterSchemaStatements(prev, plan) }
	}
	built := func(query sqlclient.Query, expected map[string]string) func() (string, error) {
		return func() (string, error) {
			sql, parameters, err := query.Build()
			assert.Equal(t, expected, parameters)
			return sql, err
		}
	}
	configured := options("serving", "etl", 2048)
	unknown := schema("serving")
	unknown.Owner, unknown.Quota = types.StringUnknown(), types.Int64Unknown()
	unmanaged := schema("serving")
	unmanaged.Owner, unmanaged.Quota = types.StringNull(), types.Int64Null()
	quoted := schemaModel{Database: types.StringValue(`it's\db`), Name: types.StringValue(`Odd"Serving`)}
	checkSQL(t, "schema", []sqlCase{
		{"create", create(schema("serving"))},
		{"create_quoted", create(schema(`Odd"Serving`))},
		{"create_owner_quota", create(options(`Odd"Serving`, `Etl"Owner`, 51200))},
		{"create_unlimited", create(options("serving", "etl", -1))},
		{"create_unknown_options", create(unknown)},
		{"create_invalid_quota", create(options("serving", "etl", 0))},
		{"alter_unchanged", alter(configured, configured)},
		{"alter_owner_quoted", alter(options(`Odd"Serving`, "etl", 2048), options(`Odd"Serving`, `New"Owner`, 2048))},
		{"alter_quota", alter(configured, options("serving", "etl", 300))},
		{"alter_quota_unlimited", alter(configured, options("serving", "etl", -1))},
		{"alter_all", alter(options("serving", "admin", -1), configured)},
		{"alter_unknown_plan", alter(configured, unknown)},
		{"alter_unmanaged", alter(configured, unmanaged)},
		{"alter_invalid_quota", alter(configured, options("serving", "etl", -5))},
		{"drop", func() string { return dropSchemaStatement(schema("serving")) }},
		{"drop_quoted", func() string { return dropSchemaStatement(schema(`Odd"Serving`)) }},
		{"read", built(readSchemaQuery(schema(`Odd"Serving`)), map[string]string{"name": `Odd"Serving`})},
		{"read_empty_name", built(readSchemaQuery(schema("")), nil)},
		{"read_quota", built(readSchemaQuotaQuery(quoted), map[string]string{"database": `it's\db`, "name": `Odd"Serving`})},
		{"read_session", built(readSchemaSessionQuery(), nil)},
	})
}
