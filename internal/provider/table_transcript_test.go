package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// tableCatalogWith returns a full fake catalog whose serving.events table is changed first.
func tableCatalogWith(change func(*tableFakeTable)) func(*catalog) {
	return func(c *catalog) { change(fakeState[*tableFake](c, "table").tables["serving.events"]) }
}

// tableFakeNote adds the nullable note column to the fake table.
func tableFakeNote(table *tableFakeTable) {
	table.columns = append(table.columns, tableFakeColumn{name: "note", dataType: "character varying(32)", encoding: "lzo"})
}

// TestTableUpdateTranscripts records the complete SQL conversation of every kind of in-place update, including
// the catalog reads before and after, so the statement order across phases is pinned.
func TestTableUpdateTranscripts(t *testing.T) {
	events := tableEventsModel()
	columns := func(extra ...tableColumnModel) types.List {
		return tableTestColumns(append([]tableColumnModel{
			tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
			tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
		}, extra...)...)
	}
	note := func(dataType, encoding string) tableColumnModel {
		return tableTestColumn("note", dataType, tableEncoded(encoding), tableNullable(true))
	}
	withNote := tableWith(events, func(m *tableModel) { m.Columns = columns(note("varchar(32)", "LZO")) })
	constrained := tableWith(events, func(m *tableModel) {
		m.Unique = tableTestUnique([]string{"label"})
		m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "accounts", "id"))
	})
	runTranscripts(t, "table_update", newTableResource, []transcriptCase{
		{name: "add_column", operation: "update", catalog: catalogWith(), prior: events, planned: withNote},
		{name: "drop_column", operation: "update", catalog: catalogWith(tableCatalogWith(tableFakeNote)), prior: withNote, planned: events},
		{name: "encode_and_widen", operation: "update", catalog: catalogWith(tableCatalogWith(tableFakeNote)), prior: withNote,
			planned: tableWith(events, func(m *tableModel) { m.Columns = columns(note("varchar(256)", "ZSTD")) })},
		{name: "diststyle_even", operation: "update", catalog: catalogWith(), prior: events,
			planned: tableWith(events, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("EVEN"), types.StringNull() })},
		{name: "sortkey_auto", operation: "update", catalog: catalogWith(), prior: events,
			planned: tableWith(events, func(m *tableModel) {
				m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
			})},
		{name: "add_constraints", operation: "update", catalog: catalogWith(), prior: events, planned: constrained},
		{name: "drop_constraints", operation: "update", catalog: catalogWith(func(c *catalog) {
			table := fakeState[*tableFake](c, "table").tables["serving.events"]
			_, _ = table.alter("events", `ADD UNIQUE ("label")`)
			_, _ = table.alter("events", `ADD FOREIGN KEY ("id") REFERENCES "serving"."accounts" ("id")`)
		}), prior: constrained, planned: events},
		{name: "owner", operation: "update", catalog: catalogWith(), prior: events,
			planned: tableWith(events, func(m *tableModel) { m.Owner = types.StringValue("analyst") })},
		{name: "sortkey_reencodes", operation: "update", catalog: catalogWith(tableCatalogWith(tableFakeNote)), prior: withNote,
			planned: tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("id", "note") })},
		{name: "drop_sortkey_column_to_auto", operation: "update", catalog: catalogWith(tableCatalogWith(func(table *tableFakeTable) {
			tableFakeNote(table)
			table.columns[0].sortKey, table.columns[2].sortKey = 0, 1
		})), prior: tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("note") }),
			planned: tableWith(events, func(m *tableModel) {
				m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
			})},
		{name: "drop_distkey_column_to_auto", operation: "update", catalog: catalogWith(tableCatalogWith(func(table *tableFakeTable) {
			tableFakeNote(table)
			table.columns[0].distKey, table.columns[2].distKey = false, true
		})), prior: tableWith(withNote, func(m *tableModel) { m.DistKey = types.StringValue("note") }),
			planned: tableWith(events, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("AUTO"), types.StringNull() })},
		{name: "import_auto_sortkey", operation: "read", catalog: catalogWith(tableCatalogWith(func(table *tableFakeTable) { table.distStyle, table.autoSortKey = "9", true })),
			prior: tableWith(events, func(m *tableModel) {
				m.DistStyle, m.DistKey = types.StringValue("AUTO"), types.StringNull()
				m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
			})},
	})
}
