package provider

import "testing"

// TestDatabasesSQL pins the database listing with and without its filters.
func TestDatabasesSQL(t *testing.T) {
	checkSQL(t, "databases", []sqlCase{
		{"all", discoverySQL(readDatabasesQuery("", ""))},
		{"type", discoverySQL(readDatabasesQuery("shared", ""))},
		{"name_like", discoverySQL(readDatabasesQuery("", "example_%"))},
		{"filtered", discoverySQL(readDatabasesQuery("local", `it's\_"%`))},
	})
}
