package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tableQuotedModel uses names and a default that need quoting and escaping. Names are lowercase, because Redshift
// folds ASCII letters of identifiers; create_invalid_uppercase pins the rejection of mixed case.
func tableQuotedModel() tableModel {
	model := tableNullModel(types.StringValue("analytics"), types.StringValue(`odd"schema`), types.StringValue(`odd"table`))
	model.Owner = types.StringValue(`odd"owner`)
	model.Columns = tableTestColumns(
		tableTestColumn(`odd"id`, "int8", tableNullable(false)),
		tableTestColumn(`path\name`, "varchar(max)", tableEncoded("ZSTD"), tableDefault(`'O''Reilly \ Co'`)),
	)
	model.PrimaryKey = tableTestList(`odd"id`)
	model.Unique = tableTestUnique([]string{`path\name`})
	model.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{`odd"id`}, `ref"schema`, `ref"table`, `ref"id`))
	model.DistKey = types.StringValue(`odd"id`)
	model.SortKey = tableTestList(`path\name`, `odd"id`)
	return model
}

// tableMinimalModel leaves every choice to Redshift.
func tableMinimalModel() tableModel {
	model := tableNullModel(types.StringValue("analytics"), types.StringValue("serving"), types.StringValue("staging"))
	model.Columns = tableTestColumns(tableTestColumn("payload", "super"))
	return model
}

// tableFullModel exercises every clause CREATE TABLE renders.
func tableFullModel() tableModel {
	model := tableNullModel(types.StringValue("analytics"), types.StringValue("serving"), types.StringValue("orders"))
	model.Columns = tableTestColumns(
		tableTestColumn("order_id", "bigint", tableIdentity(100, 10, true)),
		tableTestColumn("customer_id", "integer", tableEncoded("AZ64"), tableNullable(false)),
		tableTestColumn("amount", "decimal(12,2)", tableDefault("0")),
		tableTestColumn("created_at", "timestamp", tableEncoded("RAW"), tableNullable(false), tableDefault("getdate()")),
		tableTestColumn("note", "varchar", tableEncoded("TEXT255"), tableDefault("'n/a' -- trailing comment")),
	)
	model.PrimaryKey = tableTestList("order_id")
	model.Unique = tableTestUnique([]string{"customer_id", "created_at"}, []string{"note"})
	model.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"customer_id"}, "crm", "customers", "id"))
	model.DistStyle = types.StringValue("ALL")
	model.SortKeyStyle, model.SortKey = types.StringValue("INTERLEAVED"), tableTestList("created_at", "customer_id")
	model.Backup = types.StringValue("NO")
	return model
}

// TestTableSQL pins every table statement and catalog read against the CREATE TABLE, ALTER TABLE, and DROP TABLE
// references, including identifiers with quotes and defaults with quotes and backslashes.
func TestTableSQL(t *testing.T) {
	create := func(model tableModel) func() ([]string, error) {
		return func() ([]string, error) {
			spec, err := tableSpecOf(model)
			if err != nil {
				return nil, err
			}
			return createTableStatements(spec)
		}
	}
	events := tableEventsModel()
	eventColumns := func(extra ...tableColumnModel) types.List {
		return tableTestColumns(append([]tableColumnModel{
			tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false)),
			tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
		}, extra...)...)
	}
	note := func(dataType, encoding string) tableColumnModel {
		return tableTestColumn("note", dataType, tableEncoded(encoding), tableNullable(true))
	}
	withNote := tableWith(events, func(m *tableModel) { m.Columns = eventColumns(note("varchar(32)", "LZO")) })
	pkey := map[string]string{tablePrimaryKeyKey: "events_pkey"}
	alter := func(prev, plan tableModel, names map[string]string) func() ([]string, error) {
		return func() ([]string, error) { return alterTableStatements(prev, names, plan) }
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	quoted := tableSpecMust(t, tableQuotedModel())
	constrained := tableWith(withNote, func(m *tableModel) {
		m.Unique = tableTestUnique([]string{"label"}, []string{"id", "note"})
		m.ForeignKeys = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "accounts", "id"))
	})
	constrainedNames := map[string]string{
		tablePrimaryKeyKey: "events_pkey", tableUniqueKey([]string{"label"}): "events_label_key", tableUniqueKey([]string{"id", "note"}): `odd"key`,
		tableForeignKeyKey(tableForeignKeySpec{columns: []string{"id"}, refSchema: "serving", refTable: "accounts", refColumns: []string{"id"}}): "events_id_fkey",
	}
	checkSQL(t, "table", []sqlCase{
		{"create_events", create(events)},
		{"create_minimal", create(tableMinimalModel())},
		{"create_full", create(tableFullModel())},
		{"create_quoted", create(tableQuotedModel())},
		{"create_auto_with_owner", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Owner, m.DistStyle, m.SortKeyStyle, m.Backup = types.StringValue("etl"), types.StringValue("EVEN"), types.StringValue("AUTO"), types.StringValue("YES")
		}))},
		{"create_invalid_type", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.Columns = tableTestColumns(tableTestColumn("id", "serial")) }))},
		{"create_invalid_default", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Columns = tableTestColumns(tableTestColumn("id", "int", tableDefault("1); DROP TABLE x; --")))
		}))},
		{"create_invalid_distkey", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.DistKey = types.StringValue("missing") }))},
		{"create_invalid_distkey_type", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.DistKey = types.StringValue("payload") }))},
		{"create_invalid_sortkey_type", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Columns = tableTestColumns(tableTestColumn("id", "int"), tableTestColumn("blob", "varbyte(16)"))
			m.SortKey = tableTestList("id", "blob")
		}))},
		{"create_invalid_uppercase", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Columns = tableTestColumns(tableTestColumn(`Mixed"Case`, "int"))
		}))},
		{"alter_unchanged", alter(events, events, pkey)},
		{"alter_add_column", alter(events, withNote, pkey)},
		{"alter_add_column_with_options", alter(events, tableWith(events, func(m *tableModel) {
			m.Columns = eventColumns(tableTestColumn(`odd"note`, "varchar(8)", tableEncoded("ZSTD"), tableNullable(false), tableDefault(`'it''s \ ok'`)))
		}), pkey)},
		{"alter_drop_column", alter(withNote, events, pkey)},
		{"alter_encoding", alter(withNote, tableWith(events, func(m *tableModel) { m.Columns = eventColumns(note("varchar(32)", "ZSTD")) }), pkey)},
		{"alter_widen", alter(withNote, tableWith(events, func(m *tableModel) { m.Columns = eventColumns(note("varchar(max)", "ZSTD")) }), pkey)},
		{"alter_narrow", alter(withNote, tableWith(events, func(m *tableModel) { m.Columns = eventColumns(note("varchar(16)", "LZO")) }), pkey)},
		{"alter_diststyle_even", alter(events, tableWith(events, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("EVEN"), types.StringNull() }), pkey)},
		{"alter_diststyle_auto", alter(events, tableWith(events, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("AUTO"), types.StringNull() }), pkey)},
		{"alter_distkey", alter(withNote, tableWith(withNote, func(m *tableModel) { m.DistKey = types.StringValue("note") }), pkey)},
		{"alter_diststyle_key", alter(tableWith(withNote, func(m *tableModel) { m.DistStyle, m.DistKey = types.StringValue("ALL"), types.StringNull() }), withNote, pkey)},
		{"alter_sortkey_auto", alter(events, tableWith(events, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), pkey)},
		{"alter_sortkey_columns", alter(withNote, tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("note", "id") }), pkey)},
		{"alter_leave_interleaved", alter(tableWith(withNote, func(m *tableModel) { m.SortKeyStyle = types.StringValue("INTERLEAVED") }), tableWith(withNote, func(m *tableModel) {
			m.SortKey, m.DistStyle, m.DistKey = tableTestList("note"), types.StringValue("EVEN"), types.StringNull()
		}), pkey)},
		{"alter_interleaved", alter(events, tableWith(events, func(m *tableModel) { m.SortKeyStyle = types.StringValue("INTERLEAVED") }), pkey)},
		{"alter_add_constraints", alter(withNote, constrained, pkey)},
		{"alter_drop_constraints", alter(constrained, withNote, constrainedNames)},
		{"alter_drop_constraint_without_name", alter(constrained, withNote, pkey)},
		{"alter_primary_key", alter(tableWith(withNote, func(m *tableModel) { m.Columns = eventColumns(note("varchar(32)", "LZO")) }),
			tableWith(events, func(m *tableModel) {
				m.Columns = eventColumns(tableTestColumn("code", "char(2)", tableNullable(false), tableDefault("'xx'")))
				m.PrimaryKey = tableTestList("id", "code")
			}), pkey)},
		{"alter_add_not_null_without_default", alter(events, tableWith(events, func(m *tableModel) {
			m.Columns = eventColumns(tableTestColumn("code", "char(2)", tableNullable(false)))
		}), pkey)},
		{"alter_interleaved_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.SortKeyStyle = types.StringValue("INTERLEAVED") }), tableWith(withNote, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), pkey)},
		{"alter_drop_sortkey_column_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestList("note") }), tableWith(events, func(m *tableModel) {
			m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
		}), pkey)},
		{"alter_drop_distkey_column_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.DistKey = types.StringValue("note") }), tableWith(events, func(m *tableModel) {
			m.DistStyle, m.DistKey = types.StringValue("AUTO"), types.StringNull()
		}), pkey)},
		{"alter_owner", alter(events, tableWith(events, func(m *tableModel) { m.Owner = types.StringValue(`odd"owner`) }), pkey)},
		{"alter_unknown_owner", alter(events, tableWith(events, func(m *tableModel) { m.Owner = types.StringUnknown() }), pkey)},
		{"alter_all_phases", alter(tableWith(constrained, func(m *tableModel) { m.SortKeyStyle = types.StringValue("INTERLEAVED") }), tableWith(events, func(m *tableModel) {
			m.Owner = types.StringValue("analyst")
			m.Columns = tableTestColumns(
				tableTestColumn("id", "bigint", tableEncoded("ZSTD"), tableNullable(false), tableIdentity(1, 1, false)),
				tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
				tableTestColumn("extra", "date", tableEncoded("AZ64"), tableNullable(true)),
			)
			m.Unique = tableTestUnique([]string{"label", "extra"})
			m.DistStyle, m.DistKey = types.StringValue("KEY"), types.StringValue("extra")
			m.SortKey = tableTestList("extra")
		}), constrainedNames)},
		{"drop", func() string { return dropTableStatement(tableSpecMust(t, tableEventsModel())) }},
		{"drop_quoted", func() string { return dropTableStatement(quoted) }},
		{"read_table", built(readTableQuery(quoted))},
		{"read_attributes", built(readTableAttributesQuery(quoted))},
		{"read_columns", built(readTableColumnsQuery(`odd"database`, quoted))},
		{"read_constraints", built(readTableConstraintsQuery(quoted))},
		{"read_sortkey", built(readTableSortKeyQuery(quoted))},
	})
}

// TestTableReadQueriesBindNames sends names only as parameters.
func TestTableReadQueriesBindNames(t *testing.T) {
	quoted := tableSpecMust(t, tableQuotedModel())
	_, parameters, err := readTableColumnsQuery("analytics", quoted).Build()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"database": "analytics", "schema": `odd"schema`, "name": `odd"table`}, parameters)
	_, _, err = readTableQuery(tableSpec{schema: "serving"}).Build()
	require.ErrorContains(t, err, ":name is empty")
}
