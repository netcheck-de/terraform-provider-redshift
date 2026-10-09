package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestCommentSQL pins the COMMENT ON statement, the annotation queries and the validation message for every
// object kind.
func TestCommentSQL(t *testing.T) {
	target := func(kind, schema, name, column string) commentModel {
		return commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue(kind), ObjectName: types.StringValue(name), SchemaName: types.StringValue(schema), ColumnName: types.StringValue(column)}
	}
	set := func(data commentModel, text string) func() (string, error) {
		return func() (string, error) { return commentStatement(data, text) }
	}
	// The literal pins the escaping of quotes and backslashes, which Redshift reads as escape characters.
	annotate := func(kind, schema, name, column string) func() (string, error) {
		return set(target(kind, schema, name, column), `it's \annotated`)
	}
	read := func(kind, schema, name, column string) func() (string, error) {
		return func() (string, error) {
			query, err := readCommentQuery(target(kind, schema, name, column))
			if err != nil {
				return "", err
			}
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "comment", []sqlCase{
		{"database", annotate("DATABASE", "", "analytics", "")},
		{"schema", annotate("SCHEMA", "", "serving", "")},
		{"table", annotate("TABLE", "serving", "table", "")},
		{"view", annotate("VIEW", "serving", "view", "")},
		{"column", annotate("COLUMN", "serving", "table", "column")},
		{"quoted_identifiers", annotate("COLUMN", `odd"schema`, `odd"table`, `odd"column`)},
		{"uppercase_identifiers", annotate("TABLE", "Serving", "Orders", "")},
		{"clear", set(target("SCHEMA", "", "serving", ""), "")},
		{"clear_column", set(target("COLUMN", "serving", "table", "column"), "")},
		{"database_other_name", annotate("DATABASE", "", "other", "")},
		{"schema_with_schema_name", annotate("SCHEMA", "serving", "serving", "")},
		{"table_without_schema", annotate("TABLE", "", "table", "")},
		{"table_with_column", annotate("TABLE", "serving", "table", "column")},
		{"column_without_column", annotate("COLUMN", "serving", "table", "")},
		{"column_without_name", annotate("COLUMN", "serving", "", "column")},
		{"unsupported_kind", annotate("FUNCTION", "serving", "f", "")},
		{"read_database", read("DATABASE", "", "analytics", "")},
		{"read_schema", read("SCHEMA", "", "serving", "")},
		{"read_table", read("TABLE", "serving", "table", "")},
		{"read_view", read("VIEW", "serving", "view", "")},
		{"read_column", read("COLUMN", `odd"schema`, `Odd"Table`, `odd"column`)},
		{"read_unsupported_kind", read("FUNCTION", "serving", "f", "")},
		{"read_local_database", func() (string, error) {
			sql, _, err := commentDatabaseQuery(target("SCHEMA", "", "serving", "")).Build()
			return sql, err
		}},
	})
}
