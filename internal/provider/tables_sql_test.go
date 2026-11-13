package provider

import "testing"

// TestTablesSQL pins the table listing and its schema filter.
func TestTablesSQL(t *testing.T) {
	checkSQL(t, "tables", []sqlCase{
		{"database", discoverySQL(listTablesQuery("analytics", ""))},
		{"schema", discoverySQL(listTablesQuery("analytics", `Odd"Schema`))},
	})
}
