package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tableRenamedFrom sets a column's renamed_from hint.
func tableRenamedFrom(name string) func(*tableColumnModel) {
	return func(column *tableColumnModel) { column.RenamedFrom = types.StringValue(name) }
}

// tableRenamedEvents renames the events table's id column to event_id, including in its primary, distribution, and sort
// keys.
func tableRenamedEvents(t *testing.T) tableModel {
	t.Helper()

	model := tableEventsModel()
	columns := tableTestColumnsOf(t, model.Column)
	columns[0].Name = types.StringValue("event_id")
	columns[0].RenamedFrom = types.StringValue("id")
	model.Column = tableTestColumns(columns...)
	model.PrimaryKey = tableTestKey("event_id")
	model.Distribution = tableTestDistribution("KEY", "event_id")
	model.SortKey = tableTestSortKey("COMPOUND", "event_id")

	return model
}

// TestTableRenameSQL pins RENAME COLUMN ordering relative to key, constraint, and type changes, and checks that a
// rename never forces replacement.
func TestTableRenameSQL(t *testing.T) {
	before := tableEventsModel()
	after := tableRenamedEvents(t)
	render := func(prev, plan tableModel, constraints map[string]string) func() ([]string, error) {
		return func() ([]string, error) { return alterTableStatements(prev, constraints, plan) }
	}
	names := map[string]string{tablePrimaryKeyKey: "events_pkey"}
	removedHint := tableWith(after, func(m *tableModel) {
		columns := tableTestColumnsOf(t, m.Column)
		columns[0].RenamedFrom = types.StringNull()
		m.Column = tableTestColumns(columns...)
	})
	constrainedBefore := tableWith(before, func(m *tableModel) {
		m.Unique = tableTestUnique([]string{"id"})
		m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "events", "id"))
	})
	constrainedAfter := tableWith(after, func(m *tableModel) {
		m.Unique = tableTestUnique([]string{"event_id"})
		m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"event_id"}, "serving", "events", "event_id"))
	})
	checkSQL(t, "table_rename", []sqlCase{
		{"create_with_hint", func() ([]string, error) { return createTableStatements(tableSpecMust(t, after)) }},
		{"identity_primary_distribution_sort", render(before, after, names)},
		{"interleaved_key", render(tableWith(before, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }), tableWith(after, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "event_id") }), names)},
		{"automatic_keys", render(tableWith(before, tableAuto("id", "id")), tableWith(after, tableAuto("event_id", "event_id")), names)},
		{"already_complete", render(after, after, names)},
		{"remove_hint", render(after, removedHint, names)},
		{"constraints_preserved", render(constrainedBefore, constrainedAfter, names)},
		{"rename_then_drop_constraint", render(constrainedBefore, after, map[string]string{
			tablePrimaryKeyKey: "events_pkey", tableUniqueKey([]string{"id"}): "events_id_key",
			tableForeignKeyKey(tableForeignKeySpec{columns: []string{"id"}, refSchema: "serving", refTable: "events", refColumns: []string{"id"}}): "events_id_fkey",
		})},
		{"quoted_and_widened", render(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn(`old"name`, "varchar(16)", tableEncoded("LZO"), tableNullable(true)))
		}), tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn(`new"name`, "varchar(32)", tableEncoded("ZSTD"), tableNullable(true), tableRenamedFrom(`old"name`)))
		}), nil)},
	})
	assert.Empty(t, tableReplacements(tableSpecMust(t, before), tableSpecMust(t, after)))
}

// TestTableRenameValidation rejects self, chained, swapped, duplicate, folded, empty, and unknown rename hints, and
// fails Update without writing when the source is missing or the destination exists.
func TestTableRenameValidation(t *testing.T) {
	for name, columns := range map[string][]tableColumnModel{
		"self":      {tableTestColumn("id", "int", tableRenamedFrom("id"))},
		"chain":     {tableTestColumn("b", "int", tableRenamedFrom("a")), tableTestColumn("c", "int", tableRenamedFrom("b"))},
		"swap":      {tableTestColumn("a", "int", tableRenamedFrom("b")), tableTestColumn("b", "int", tableRenamedFrom("a"))},
		"duplicate": {tableTestColumn("a", "int", tableRenamedFrom("old")), tableTestColumn("b", "int", tableRenamedFrom("old"))},
		"uppercase": {tableTestColumn("a", "int", tableRenamedFrom("OLD"))},
		"empty":     {tableTestColumn("a", "int", tableRenamedFrom(""))},
		"unknown":   {tableTestColumn("a", "int", func(c *tableColumnModel) { c.RenamedFrom = types.StringUnknown() })},
	} {
		t.Run(name, func(t *testing.T) {
			model := tableMinimalModel()
			model.Column = tableTestColumns(columns...)
			_, err := tableSpecOf(model)
			require.Error(t, err)
		})
	}

	for _, destination := range []bool{false, true} {
		c := fullCatalog()
		table := fakeState[*tableFake](c, "table").tables["serving.events"]
		plan := tableRenamedEvents(t)

		if destination {
			table.columns = append(table.columns, tableFakeColumn{name: "event_id", dataType: "bigint", notNull: true, encoding: "az64"})
		} else {
			columns := tableTestColumnsOf(t, plan.Column)
			columns[0].RenamedFrom = types.StringValue("missing")
			plan.Column = tableTestColumns(columns...)
		}

		r := newTableResource()
		configureTestResource(t, r, c)
		_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), plan, nil)
		require.True(t, diagnostics.HasError())
		assert.Contains(t, diagnostics[0].Detail(), "source")
		assert.Empty(t, c.writes)
	}
}

// TestTableRenameRetryAndRead treats a rename already applied before a lost response as complete, keeps the configured
// hint on Read, and leaves it null in lookups.
func TestTableRenameRetryAndRead(t *testing.T) {
	c := fullCatalog()
	r := newTableResource()
	configureTestResource(t, r, c)
	plan := tableRenamedEvents(t)
	_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), plan, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{`ALTER TABLE "serving"."events" RENAME COLUMN "id" TO "event_id"`}, c.writes)
	// Simulate a lost response after the rename: state still has the source name, catalog already has the destination.
	_, diagnostics = applyOperation(t, r, "update", tableEventsModel(), plan, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Len(t, c.writes, 1)

	resource := r.(*tableResource)
	found, _, err := resource.read(context.Background(), &plan)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "id", tableTestColumnsOf(t, plan.Column)[0].RenamedFrom.ValueString())
	lookup := tableNullModel(plan.Database, plan.Schema, plan.Name)
	found, _, err = resource.read(context.Background(), &lookup)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, tableTestColumnsOf(t, lookup.Column)[0].RenamedFrom.IsNull())
}

// TestTableRenamePartialFailure resumes an interrupted multi-column rename without repeating the rename that already
// succeeded.
func TestTableRenamePartialFailure(t *testing.T) {
	c := fullCatalog()
	plan := tableRenamedEvents(t)
	columns := tableTestColumnsOf(t, plan.Column)
	columns[1].Name, columns[1].RenamedFrom = types.StringValue("description"), types.StringValue("label")
	plan.Column = tableTestColumns(columns...)
	fail := true
	r := &tableResource{testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if fail && strings.Contains(sql, `RENAME COLUMN "label"`) {
			return nil, errors.New("interrupted second rename")
		}

		return c.Query(ctx, connection, sql, parameters)
	}))}
	_, diagnostics := applyOperation(t, r, "update", tableEventsModel(), plan, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), "interrupted second rename")
	assert.Len(t, c.writes, 1)

	fail = false
	_, diagnostics = applyOperation(t, r, "update", tableEventsModel(), plan, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{
		`ALTER TABLE "serving"."events" RENAME COLUMN "id" TO "event_id"`,
		`ALTER TABLE "serving"."events" RENAME COLUMN "label" TO "description"`,
	}, c.writes)
}

// TestTableObservedSpecIgnoresMigrationHints lets the observed spec hold a rename source beside its destination, so
// rename planning, not spec validation, reports the conflict.
func TestTableObservedSpecIgnoresMigrationHints(t *testing.T) {
	model := tableRenamedEvents(t)
	columns := tableTestColumnsOf(t, model.Column)
	columns = append(columns, tableTestColumn("id", "bigint"))
	model.Column = tableTestColumns(columns...)
	_, err := tableSpecOf(model)
	require.ErrorContains(t, err, "also a configured column name")
	observed, err := tableObservedSpecOf(model)
	require.NoError(t, err)
	_, _, err = tableRenameSpec(observed, tableSpecMust(t, tableRenamedEvents(t)))
	require.ErrorContains(t, err, "both source and destination exist")
	_, _, err = tableAlterStates(model, nil, tableRenamedEvents(t))
	require.ErrorContains(t, err, "both source and destination exist")
}
