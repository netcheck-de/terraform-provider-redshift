package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestDatashareTableSQL pins adding and removing a relation, the member lookup, and the rejection of empty name
// parts that would change which relation is addressed.
func TestDatashareTableSQL(t *testing.T) {
	member := func(share, schema, table string) datashareTableModel {
		return datashareTableModel{Database: types.StringValue("analytics"), Datashare: types.StringValue(share), Schema: types.StringValue(schema), Table: types.StringValue(table)}
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	add := func(data datashareTableModel) func() (string, error) {
		return func() (string, error) { return createDatashareTableStatement(data) }
	}
	remove := func(data datashareTableModel) func() (string, error) {
		return func() (string, error) { return dropDatashareTableStatement(data) }
	}
	checkSQL(t, "datashare_table", []sqlCase{
		{"create", add(member("producer", "serving", "table"))},
		{"create_quoted", add(member(`Odd"Producer`, `Odd"Serving`, `Odd"Table`))},
		{"create_empty_schema", add(member("producer", "", "table"))},
		{"create_empty_table", add(member("producer", "serving", ""))},
		{"drop", remove(member("producer", "serving", "table"))},
		{"drop_quoted", remove(member(`Odd"Producer`, `Odd"Serving`, `Odd"Table`))},
		{"drop_empty_schema", remove(member("producer", "", "table"))},
		{"read", built(readDatashareTableQuery(member(`Odd"Producer`, `Odd"Serving`, `Odd"Table`)))},
	})
}
