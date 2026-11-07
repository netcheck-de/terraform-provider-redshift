package provider

import (
	"fmt"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	model.Column = tableTestColumns(
		tableTestColumn(`odd"id`, "int8", tableNullable(false)),
		tableTestColumn(`path\name`, "varchar(max)", tableEncoded("ZSTD"), tableDefault(`'O''Reilly \ Co'`)),
	)
	model.PrimaryKey = tableTestKey(`odd"id`)
	model.Unique = tableTestUnique([]string{`path\name`})
	model.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{`odd"id`}, `ref"schema`, `ref"table`, `ref"id`))
	model.Distribution = tableTestDistribution("", `odd"id`)
	model.SortKey = tableTestSortKey("", `path\name`, `odd"id`)

	return model
}

// tableMinimalModel leaves every choice to Redshift.
func tableMinimalModel() tableModel {
	model := tableNullModel(types.StringValue("analytics"), types.StringValue("serving"), types.StringValue("staging"))
	model.Column = tableTestColumns(tableTestColumn("payload", "super"))

	return model
}

// tableFullModel exercises every clause CREATE TABLE renders.
func tableFullModel() tableModel {
	model := tableNullModel(types.StringValue("analytics"), types.StringValue("serving"), types.StringValue("orders"))
	model.Column = tableTestColumns(
		tableTestColumn("order_id", "bigint", tableIdentity(100, 10, true)),
		tableTestColumn("customer_id", "integer", tableEncoded("AZ64"), tableNullable(false)),
		tableTestColumn("amount", "decimal(12,2)", tableDefault("0")),
		tableTestColumn("created_at", "timestamp", tableEncoded("RAW"), tableNullable(false), tableDefault("getdate()")),
		tableTestColumn("note", "varchar", tableEncoded("TEXT255"), tableDefault("'n/a' -- trailing comment")),
	)
	model.PrimaryKey = tableTestKey("order_id")
	model.Unique = tableTestUnique([]string{"customer_id", "created_at"}, []string{"note"})
	model.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"customer_id"}, "crm", "customers", "id"))
	model.Distribution = tableTestDistribution("ALL", "")
	model.SortKey = tableTestSortKey("INTERLEAVED", "created_at", "customer_id")
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
	id := tableTestColumn("id", "bigint", tableEncoded("AZ64"), tableNullable(false), tableIdentity(1, 1, false))
	label := tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'"))
	eventColumns := func(extra ...tableColumnModel) types.List {
		return tableTestColumns(append([]tableColumnModel{id, label}, extra...)...)
	}
	note := func(dataType, encoding string) tableColumnModel {
		return tableTestColumn("note", dataType, tableEncoded(encoding), tableNullable(true))
	}
	withNote := tableWith(events, func(m *tableModel) { m.Column = eventColumns(note("varchar(32)", "LZO")) })
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
		m.ForeignKey = tableTestForeignKeys(tableTestForeignKey([]string{"id"}, "serving", "accounts", "id"))
	})
	constrainedNames := map[string]string{
		tablePrimaryKeyKey: "events_pkey", tableUniqueKey([]string{"label"}): "events_label_key", tableUniqueKey([]string{"id", "note"}): `odd"key`,
		tableForeignKeyKey(tableForeignKeySpec{columns: []string{"id"}, refSchema: "serving", refTable: "accounts", refColumns: []string{"id"}}): "events_id_fkey",
	}
	autoSort := func(m *tableModel) { m.SortKey = types.ObjectNull(tableSortKeyAttributeTypes) }
	autoDistribution := func(m *tableModel) { m.Distribution = types.ObjectNull(tableDistributionAttributeTypes) }
	wide := tableTestWideModel(events, tableEncoded("AZ64"), tableNullable(true))
	wideColumns := wide.Column.Elements()
	limitPlan := tableTestWithColumnValues(events, slices.Concat(wideColumns[:2], wideColumns[3:tableMaxColumns-1],
		tableTestColumns(tableTestColumn("extra", "int", tableNullable(true))).Elements()))
	checkSQL(t, "table", []sqlCase{
		{"create_events", create(events)},
		{"create_minimal", create(tableMinimalModel())},
		{"create_full", create(tableFullModel())},
		{"create_quoted", create(tableQuotedModel())},
		{"create_auto_with_owner", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Owner, m.Distribution, m.SortKey, m.Backup = types.StringValue("etl"), tableTestDistribution("EVEN", ""), tableTestSortKey("AUTO"), types.StringValue("YES")
		}))},
		{"create_sortkey_none", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Owner, m.SortKey = types.StringValue("etl"), tableTestSortKey("NONE")
		}))},
		{"create_invalid_type", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.Column = tableTestColumns(tableTestColumn("id", "serial")) }))},
		{"create_invalid_default", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "int", tableDefault("1); DROP TABLE x; --")))
		}))},
		{"create_invalid_distkey", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.Distribution = tableTestDistribution("", "missing") }))},
		{"create_invalid_distkey_type", create(tableWith(tableMinimalModel(), func(m *tableModel) { m.Distribution = tableTestDistribution("", "payload") }))},
		{"create_invalid_sortkey_type", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "int"), tableTestColumn("blob", "varbyte(16)"))
			m.SortKey = tableTestSortKey("", "id", "blob")
		}))},
		{"create_invalid_uppercase", create(tableWith(tableMinimalModel(), func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn(`Mixed"Case`, "int"))
		}))},
		{"alter_unchanged", alter(events, events, pkey)},
		{"alter_add_column", alter(events, withNote, pkey)},
		{"alter_add_column_with_options", alter(events, tableWith(events, func(m *tableModel) {
			m.Column = eventColumns(tableTestColumn(`odd"note`, "varchar(8)", tableEncoded("ZSTD"), tableNullable(false), tableDefault(`'it''s \ ok'`)))
		}), pkey)},
		{"alter_insert_middle_column", alter(events, tableWith(events, func(m *tableModel) { m.Column = tableTestColumns(id, note("varchar(32)", "LZO"), label) }), pkey)},
		{"alter_reorder_columns", alter(withNote, tableWith(events, func(m *tableModel) { m.Column = tableTestColumns(note("varchar(32)", "LZO"), label, id) }), pkey)},
		{"alter_drop_column", alter(withNote, events, pkey)},
		{"alter_drop_middle_column", alter(tableWith(events, func(m *tableModel) { m.Column = tableTestColumns(id, note("varchar(32)", "LZO"), label) }), events, pkey)},
		{"alter_drop_key_column", alter(tableWith(withNote, func(m *tableModel) {
			m.Distribution, m.SortKey = tableTestDistribution("", "note"), tableTestSortKey("", "note")
		}), events, pkey)},
		{"alter_drop_auto_key_column", alter(tableWith(withNote, tableAuto("note", "note", "id")), tableWith(events, func(m *tableModel) {
			tableAuto("", "")(m)
			m.EffectiveDistribution, m.EffectiveSortKey = types.ObjectUnknown(tableEffectiveDistributionAttributeTypes), types.ObjectUnknown(tableEffectiveSortKeyAttributeTypes)
		}), pkey)},
		{"alter_drop_auto_distkey_column_interleaved", alter(tableWith(withNote, func(m *tableModel) {
			tableAuto("note")(m)
			m.SortKey = tableTestSortKey("INTERLEAVED", "id")
		}), tableWith(events, func(m *tableModel) {
			tableAuto("", "")(m)
			m.SortKey = tableTestSortKey("INTERLEAVED", "id")
		}), pkey)},
		{"alter_drop_column_keeps_auto_keys", alter(tableWith(withNote, tableAuto("id", "id")), tableWith(events, tableAuto("id", "id")), pkey)},
		{"alter_column_limit", alter(wide, limitPlan, pkey)},
		{"alter_encoding", alter(withNote, tableWith(events, func(m *tableModel) { m.Column = eventColumns(note("varchar(32)", "ZSTD")) }), pkey)},
		{"alter_widen", alter(withNote, tableWith(events, func(m *tableModel) { m.Column = eventColumns(note("varchar(max)", "ZSTD")) }), pkey)},
		{"alter_narrow", alter(withNote, tableWith(events, func(m *tableModel) { m.Column = eventColumns(note("varchar(16)", "LZO")) }), pkey)},
		{"alter_diststyle_even", alter(events, tableWith(events, func(m *tableModel) { m.Distribution = tableTestDistribution("EVEN", "") }), pkey)},
		{"alter_diststyle_auto", alter(events, tableWith(events, autoDistribution), pkey)},
		{"alter_distkey", alter(withNote, tableWith(withNote, func(m *tableModel) { m.Distribution = tableTestDistribution("", "note") }), pkey)},
		{"alter_diststyle_key", alter(tableWith(withNote, func(m *tableModel) { m.Distribution = tableTestDistribution("ALL", "") }), withNote, pkey)},
		{"alter_sortkey_auto", alter(events, tableWith(events, autoSort), pkey)},
		{"alter_sortkey_none", alter(events, tableWith(events, func(m *tableModel) { m.SortKey = tableTestSortKey("NONE") }), pkey)},
		{"alter_sortkey_columns", alter(withNote, tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("", "note", "id") }), pkey)},
		{"alter_leave_interleaved", alter(tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }), tableWith(withNote, func(m *tableModel) {
			m.SortKey, m.Distribution = tableTestSortKey("", "note"), tableTestDistribution("EVEN", "")
		}), pkey)},
		{"alter_interleaved", alter(events, tableWith(events, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }), pkey)},
		{"alter_add_constraints", alter(withNote, constrained, pkey)},
		{"alter_drop_constraints", alter(constrained, withNote, constrainedNames)},
		{"alter_drop_constraint_without_name", alter(constrained, withNote, pkey)},
		{"alter_primary_key", alter(withNote, tableWith(events, func(m *tableModel) {
			m.Column = eventColumns(tableTestColumn("code", "char(2)", tableNullable(false), tableDefault("'xx'")))
			m.PrimaryKey = tableTestKey("id", "code")
		}), pkey)},
		{"alter_add_not_null_without_default", alter(events, tableWith(events, func(m *tableModel) {
			m.Column = eventColumns(tableTestColumn("code", "char(2)", tableNullable(false)))
		}), pkey)},
		{"alter_interleaved_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }), tableWith(withNote, autoSort), pkey)},
		{"alter_drop_sortkey_column_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.SortKey = tableTestSortKey("", "note") }), tableWith(events, autoSort), pkey)},
		{"alter_drop_distkey_column_to_auto", alter(tableWith(withNote, func(m *tableModel) { m.Distribution = tableTestDistribution("", "note") }), tableWith(events, autoDistribution), pkey)},
		{"alter_owner", alter(events, tableWith(events, func(m *tableModel) { m.Owner = types.StringValue(`odd"owner`) }), pkey)},
		{"alter_unknown_owner", alter(events, tableWith(events, func(m *tableModel) { m.Owner = types.StringUnknown() }), pkey)},
		{"alter_all_phases", alter(tableWith(constrained, func(m *tableModel) { m.SortKey = tableTestSortKey("INTERLEAVED", "id") }), tableWith(events, func(m *tableModel) {
			m.Owner = types.StringValue("analyst")
			m.Column = tableTestColumns(
				tableTestColumn("id", "bigint", tableEncoded("ZSTD"), tableNullable(false), tableIdentity(1, 1, false)),
				tableTestColumn("label", "varchar(64)", tableEncoded("LZO"), tableNullable(true), tableDefault("'none'")),
				tableTestColumn("extra", "date", tableEncoded("AZ64"), tableNullable(true)),
			)
			m.Unique = tableTestUnique([]string{"label", "extra"})
			m.Distribution = tableTestDistribution("KEY", "extra")
			m.SortKey = tableTestSortKey("", "extra")
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

// TestTableSQLRequiresKnownDefinitions refuses to render a definition, constraint, or alteration from unknown values.
func TestTableSQLRequiresKnownDefinitions(t *testing.T) {
	m := tableMinimalModel()
	m.Schema = types.StringUnknown()
	_, err := tableSpecOf(m)
	require.ErrorContains(t, err, "schema")

	m = tableMinimalModel()
	m.Column = types.ListValueMust(types.ObjectType{AttrTypes: tableColumnAttributeTypes}, []attr.Value{types.ObjectUnknown(tableColumnAttributeTypes)})
	_, err = tableSpecOf(m)
	require.ErrorContains(t, err, "column must be known")

	c := tableTestColumn("id", "integer")
	c.Name = types.StringUnknown()
	_, err = tableColumnSpecOf(c)
	require.ErrorContains(t, err, "name")
	_, err = tableUniqueOf(types.SetValueMust(types.ObjectType{AttrTypes: tableKeyAttributeTypes}, []attr.Value{types.ObjectUnknown(tableKeyAttributeTypes)}))
	require.ErrorContains(t, err, "unique must be known")
	_, err = tableForeignKeysOf(tableTestForeignKeys(types.ObjectUnknown(tableForeignKeyAttributeTypes)))
	require.ErrorContains(t, err, "foreign_key must be known")

	for _, refs := range []types.Object{
		tableTestReferences(types.StringUnknown(), types.StringValue("t"), tableTestList("id")),
		tableTestReferences(types.StringValue("s"), types.StringValue("t"), types.ListUnknown(types.StringType)),
	} {
		_, err = tableForeignKeysOf(tableTestForeignKeys(types.ObjectValueMust(tableForeignKeyAttributeTypes, map[string]attr.Value{"columns": tableTestList("payload"), "references": refs})))
		require.Error(t, err)
	}

	keys, err := tableForeignKeysOf(tableTestForeignKeys(tableTestForeignKey([]string{"a"}, "s", "t", "a"), tableTestForeignKey([]string{"b"}, "s", "t", "b")))
	require.NoError(t, err)
	assert.Len(t, keys, 2)

	names := make([]string, tableMaxCompoundSortKeys+1)
	for i := range names {
		names[i] = fmt.Sprintf("c%d", i)
	}

	var spec tableSpec
	require.ErrorContains(t, spec.resolveSortKey(tableTestSortKey("COMPOUND", names...)), "at most")

	valid := tableMinimalModel()
	_, _, err = tableAlterStates(m, nil, valid)
	require.ErrorContains(t, err, "current table definition")
	_, _, err = tableAlterStates(valid, nil, m)
	require.Error(t, err)
}

// TestTableCatalogRejectsMalformedValues rejects identity, column-list, and security values the catalog cannot have produced.
func TestTableCatalogRejectsMalformedValues(t *testing.T) {
	assert.Nil(t, tableParseIdentity("identity(123, 1, '99999999999999999999999999999,1'::text)"))

	_, _, err := tableIdentifierList("id", 0)
	require.ErrorContains(t, err, "expected a column list")
	_, _, err = tableIdentifierList("", 0)
	require.Error(t, err)

	m := tableMinimalModel()
	m.SortKey = types.ObjectUnknown(tableSortKeyAttributeTypes)
	assert.Empty(t, tablePriorSortStyle(m))

	_, err = tableSecurityDatashareStatement(tableSecurityModel{})
	require.ErrorContains(t, err, "nonempty schema")
}
