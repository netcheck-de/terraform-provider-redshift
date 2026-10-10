package provider

import "testing"

// TestColumnsSQL pins the column listing with and without its relation filters.
func TestColumnsSQL(t *testing.T) {
	checkSQL(t, "columns", []sqlCase{
		{"database", discoverySQL(readColumnsQuery("analytics", "", ""))},
		{"table", discoverySQL(readColumnsQuery("analytics", `Odd"Schema`, "Orders"))},
		{"table_without_schema", discoverySQL(readColumnsQuery("analytics", "", "orders"))},
	})
}
