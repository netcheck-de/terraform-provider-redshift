package provider

import "testing"

// TestDatashareObjectsSQL pins the member listing query with and without datashare and object type filters.
func TestDatashareObjectsSQL(t *testing.T) {
	checkSQL(t, "datashare_objects", []sqlCase{
		{"all", func() (string, error) {
			sql, _, err := listDatashareObjectsQuery("admin", "producer", "").Build()
			return sql, err
		}},
		{"functions", func() (string, error) {
			sql, _, err := listDatashareObjectsQuery("admin", `Odd"Share`, "function").Build()
			return sql, err
		}},
		{"all_shares", func() (string, error) {
			sql, _, err := listDatashareObjectsQuery("admin", "", "").Build()
			return sql, err
		}},
		{"empty_database", func() (string, error) {
			sql, _, err := listDatashareObjectsQuery("", "producer", "").Build()
			return sql, err
		}},
	})
}
