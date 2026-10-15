package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTablesSQL pins the relation listing, its filters, and the materialized view lookup.
func TestTablesSQL(t *testing.T) {
	checkSQL(t, "tables", []sqlCase{
		{"database", discoverySQL(readTablesQuery("analytics", "", ""))},
		{"schema", discoverySQL(readTablesQuery("analytics", `Odd"Schema`, ""))},
		{"table_type", discoverySQL(readTablesQuery("analytics", "serving", tablesTypeExternal))},
		{"materialized_type", discoverySQL(readTablesQuery("analytics", "serving", tablesTypeMaterialized))},
		{"materialized", discoverySQL(readTablesMaterializedQuery("analytics", ""))},
		{"materialized_schema", discoverySQL(readTablesMaterializedQuery("analytics", "serving"))},
	})
}

// TestTablesMaterializedLookup runs the SVV_MV_INFO read only when the listing can contain views.
func TestTablesMaterializedLookup(t *testing.T) {
	for tableType, expected := range map[string]bool{"": true, tablesTypeView: true, tablesTypeMaterialized: true, tablesTypeTable: false, tablesTypeExternal: false, tablesTypeShared: false} {
		assert.Equal(t, expected, tablesNeedMaterialized(tableType), tableType)
	}
}
