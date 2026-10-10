package provider

import "testing"

// TestSchemasSQL pins the schema listing with and without its type filter.
func TestSchemasSQL(t *testing.T) {
	checkSQL(t, "schemas", []sqlCase{
		{"database", discoverySQL(readSchemasQuery("analytics", ""))},
		{"type", discoverySQL(readSchemasQuery(`Odd"Database`, "external"))},
		{"empty_database", discoverySQL(readSchemasQuery("", ""))},
	})
}
