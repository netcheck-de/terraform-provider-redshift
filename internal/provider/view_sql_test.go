package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testViewModel returns the representative view of the fake catalog.
func testViewModel() viewModel {
	return viewModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue(fakeViewSchema), Name: types.StringValue(fakeViewName),
		Query: types.StringValue(fakeViewQuery), LateBinding: types.BoolValue(false), Owner: types.StringValue(fakeViewOwner), DefinitionFingerprint: types.StringNull(),
	}
}

// TestViewSQL pins every view statement, including identifier and literal quoting and user SQL edge cases.
func TestViewSQL(t *testing.T) {
	with := func(change func(*viewModel)) viewModel {
		data := testViewModel()
		change(&data)
		return data
	}
	quoted := with(func(data *viewModel) {
		data.Schema, data.Name, data.Owner = types.StringValue(`Odd"Schema`), types.StringValue(`My"View`), types.StringValue(`Owner"X`)
		data.Query = types.StringValue(`SELECT 'it''s \new' AS "Quoted""Col" FROM "Odd""Schema".t`)
	})
	late := with(func(data *viewModel) { data.LateBinding = types.BoolValue(true) })
	unowned := with(func(data *viewModel) { data.Owner = types.StringNull() })
	create := func(data viewModel) func() ([]string, error) {
		return func() ([]string, error) { return createViewStatements(data) }
	}
	alter := func(prev, plan viewModel) func() ([]string, error) {
		return func() ([]string, error) { return alterViewStatements(prev, plan) }
	}
	changedQuery := with(func(data *viewModel) {
		data.Query = types.StringValue("SELECT id, label, 1 AS version FROM serving.sales")
	})
	checkSQL(t, "view", []sqlCase{
		{"create", create(testViewModel())},
		{"create_unowned", create(unowned)},
		{"create_unknown_owner", create(with(func(data *viewModel) { data.Owner = types.StringUnknown() }))},
		{"create_late_binding", create(late)},
		{"create_quoted", create(quoted)},
		{"create_heredoc_query", create(with(func(data *viewModel) { data.Query = types.StringValue("\n  SELECT id\n  FROM serving.sales\n") }))},
		{"create_trailing_line_comment", create(with(func(data *viewModel) {
			data.Query, data.LateBinding = types.StringValue("SELECT id FROM serving.sales -- latest"), types.BoolValue(true)
		}))},
		{"create_semicolon", create(with(func(data *viewModel) { data.Query = types.StringValue("SELECT 1; DROP TABLE serving.sales") }))},
		{"create_unterminated_literal", create(with(func(data *viewModel) { data.Query = types.StringValue("SELECT 'open") }))},
		{"create_parameter", create(with(func(data *viewModel) { data.Query = types.StringValue("SELECT :name") }))},
		{"create_empty_query", create(with(func(data *viewModel) { data.Query = types.StringValue("  ") }))},
		{"create_empty_schema", create(with(func(data *viewModel) { data.Schema = types.StringValue("") }))},
		{"alter_unchanged", alter(testViewModel(), testViewModel())},
		{"alter_query", alter(testViewModel(), changedQuery)},
		{"alter_late_binding", alter(testViewModel(), late)},
		{"alter_query_and_late_binding", alter(testViewModel(), with(func(data *viewModel) {
			data.Query, data.LateBinding = changedQuery.Query, types.BoolValue(true)
		}))},
		{"alter_owner", alter(testViewModel(), with(func(data *viewModel) { data.Owner = types.StringValue("reporter") }))},
		{"alter_query_and_owner_quoted", alter(with(func(data *viewModel) { data.Schema, data.Name = quoted.Schema, quoted.Name }), quoted)},
		{"alter_owner_unset", alter(testViewModel(), unowned)},
		{"alter_invalid_query", alter(testViewModel(), with(func(data *viewModel) { data.Query = types.StringValue("SELECT (1") }))},
		{"drop", func() (string, error) { return dropViewStatement(testViewModel()) }},
		{"drop_quoted", func() (string, error) { return dropViewStatement(quoted) }},
		{"drop_empty_name", func() (string, error) {
			return dropViewStatement(with(func(data *viewModel) { data.Name = types.StringValue("") }))
		}},
		{"read", func() (string, error) {
			sql, parameters, err := readViewQuery(quoted).Build()
			assert.Equal(t, map[string]string{"schema": `Odd"Schema`, "name": `My"View`}, parameters)
			return sql, err
		}},
	})
}

// TestViewAlterCoverage keeps an update step for every in-place view attribute.
func TestViewAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newViewResource(), viewAlterSteps)
}

// TestViewReadQueryRejectsEmptyName fails before sending a binding the Data API would reject.
func TestViewReadQueryRejectsEmptyName(t *testing.T) {
	_, _, err := readViewQuery(viewModel{Schema: types.StringValue("serving"), Name: types.StringValue("")}).Build()
	require.ErrorContains(t, err, ":name is empty")
}

// TestViewDefinitionKind classifies the pg_get_viewdef forms the CREATE VIEW and CREATE MATERIALIZED VIEW pages show.
func TestViewDefinitionKind(t *testing.T) {
	for definition, expected := range map[string][2]bool{
		" SELECT venue.venueid FROM venue;":                                                       {false, false},
		"create view sales_vw_lbv as select * from public.sales with no schema binding;":          {true, false},
		"CREATE VIEW x AS SELECT 1\nWITH NO SCHEMA BINDING":                                       {true, false},
		"create materialized view mv_sales_vw as select a from t;":                                {false, true},
		"  CREATE MATERIALIZED VIEW mv AS SELECT 'with no schema binding' AS note FROM t;":        {false, true},
		"SELECT 'with no schema binding' AS note;":                                                {false, false},
		"SELECT note FROM t WHERE note = 'create materialized view x'":                            {false, false},
		"create view lbv as select * from public.events with  no\tschema\nbinding  ;  ":           {true, false},
		"create view lbv as select 'trailing' as note from public.events with no schema bindings": {false, false},
	} {
		lateBinding, materialized := viewDefinitionKind(definition)
		assert.Equal(t, expected, [2]bool{lateBinding, materialized}, definition)
	}
}
