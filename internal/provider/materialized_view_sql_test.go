package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMaterializedViewModel returns the representative materialized view of the fake catalog.
func testMaterializedViewModel() materializedViewModel {
	return materializedViewModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue(fakeViewSchema), Name: types.StringValue(fakeMaterializedViewName),
		Query: types.StringValue(fakeMaterializedQuery), Backup: types.BoolNull(), DistStyle: types.StringNull(), DistKey: types.StringNull(),
		SortKey: types.ListNull(types.StringType), AutoRefresh: types.BoolValue(true), Owner: types.StringValue(fakeViewOwner), DefinitionFingerprint: types.StringNull(),
	}
}

// materializedViewSortKeyValue builds a sort key list.
func materializedViewSortKeyValue(columns ...string) types.List {
	elements := make([]attr.Value, len(columns))
	for i, column := range columns {
		elements[i] = types.StringValue(column)
	}
	return types.ListValueMust(types.StringType, elements)
}

// TestMaterializedViewSQL pins every materialized view statement, including clause order, quoting, and the
// storage option combinations Redshift rejects.
func TestMaterializedViewSQL(t *testing.T) {
	with := func(change func(*materializedViewModel)) materializedViewModel {
		data := testMaterializedViewModel()
		change(&data)
		return data
	}
	quoted := with(func(data *materializedViewModel) {
		data.Schema, data.Name, data.Owner = types.StringValue(`Odd"Schema`), types.StringValue(`My"Summary`), types.StringValue(`Owner"X`)
		data.DistKey, data.SortKey = types.StringValue(`Label"Col`), materializedViewSortKeyValue(`Label"Col`, "ID")
		data.Query = types.StringValue(`SELECT "Label""Col", 'it''s \new' AS note, id AS "ID" FROM "Odd""Schema".t`)
	})
	create := func(data materializedViewModel) func() ([]string, error) {
		return func() ([]string, error) { return createMaterializedViewStatements(data) }
	}
	alter := func(prev, plan materializedViewModel) func() ([]string, error) {
		return func() ([]string, error) { return alterMaterializedViewStatements(prev, plan) }
	}
	checkSQL(t, "materialized_view", []sqlCase{
		{"create", create(testMaterializedViewModel())},
		{"create_minimal", create(with(func(data *materializedViewModel) {
			data.AutoRefresh, data.Owner = types.BoolValue(false), types.StringUnknown()
		}))},
		{"create_all_options", create(with(func(data *materializedViewModel) {
			data.Backup, data.DistStyle, data.DistKey = types.BoolValue(false), types.StringValue("KEY"), types.StringValue("label")
			data.SortKey = materializedViewSortKeyValue("label", "sales")
		}))},
		{"create_backup_yes_diststyle_all", create(with(func(data *materializedViewModel) {
			data.Backup, data.DistStyle = types.BoolValue(true), types.StringValue("all")
		}))},
		{"create_distkey_without_style", create(with(func(data *materializedViewModel) { data.DistKey = types.StringValue("label") }))},
		{"create_quoted", create(quoted)},
		{"create_distkey_with_even", create(with(func(data *materializedViewModel) {
			data.DistStyle, data.DistKey = types.StringValue("EVEN"), types.StringValue("label")
		}))},
		{"create_key_without_distkey", create(with(func(data *materializedViewModel) { data.DistStyle = types.StringValue("KEY") }))},
		{"create_auto_diststyle", create(with(func(data *materializedViewModel) { data.DistStyle = types.StringValue("AUTO") }))},
		{"create_empty_sortkey_column", create(with(func(data *materializedViewModel) { data.SortKey = materializedViewSortKeyValue("label", " ") }))},
		{"create_semicolon", create(with(func(data *materializedViewModel) { data.Query = types.StringValue("SELECT 1; SELECT 2") }))},
		{"create_empty_name", create(with(func(data *materializedViewModel) { data.Name = types.StringValue("") }))},
		{"alter_unchanged", alter(testMaterializedViewModel(), testMaterializedViewModel())},
		{"alter_auto_refresh_off", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.AutoRefresh = types.BoolValue(false) }))},
		{"alter_quoted_all_in_place_options", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.AutoRefresh = quoted.Schema, quoted.Name, types.BoolValue(false)
		}), quoted)},
		{"alter_adopts_imported_options", alter(with(func(data *materializedViewModel) { data.Query = types.StringNull() }), with(func(data *materializedViewModel) {
			data.Backup, data.SortKey = types.BoolValue(false), materializedViewSortKeyValue("label")
		}))},
		{"alter_diststyle_all", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.DistStyle = types.StringValue("ALL") }))},
		{"alter_distkey_quoted", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.DistStyle = quoted.Schema, quoted.Name, types.StringValue("ALL")
		}), with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.DistKey = quoted.Schema, quoted.Name, quoted.DistKey
		}))},
		{"alter_distribution_removed", alter(with(func(data *materializedViewModel) {
			data.DistStyle, data.DistKey = types.StringValue("KEY"), types.StringValue("label")
		}), testMaterializedViewModel())},
		{"alter_explicit_key_style_unchanged", alter(with(func(data *materializedViewModel) { data.DistKey = types.StringValue("label") }), with(func(data *materializedViewModel) {
			data.DistStyle, data.DistKey = types.StringValue("KEY"), types.StringValue("label")
		}))},
		{"alter_distkey_with_all", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) {
			data.DistStyle, data.DistKey = types.StringValue("ALL"), types.StringValue("label")
		}))},
		{"alter_sortkey_quoted", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name = quoted.Schema, quoted.Name
		}), with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.SortKey = quoted.Schema, quoted.Name, quoted.SortKey
		}))},
		{"alter_sortkey_removed", alter(with(func(data *materializedViewModel) { data.SortKey = materializedViewSortKeyValue("label") }), testMaterializedViewModel())},
		{"alter_storage_then_owner", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) {
			data.DistStyle, data.SortKey, data.AutoRefresh, data.Owner = types.StringValue("EVEN"), materializedViewSortKeyValue("label"), types.BoolValue(false), types.StringValue("reporter")
		}))},
		{"alter_owner_unset", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.Owner = types.StringNull() }))},
		{"alter_empty_schema", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.Schema = types.StringValue("") }))},
		{"drop", func() (string, error) { return dropMaterializedViewStatement(testMaterializedViewModel()) }},
		{"drop_quoted", func() (string, error) { return dropMaterializedViewStatement(quoted) }},
		{"drop_empty_name", func() (string, error) {
			return dropMaterializedViewStatement(with(func(data *materializedViewModel) { data.Name = types.StringValue("") }))
		}},
		{"read", func() (string, error) {
			sql, parameters, err := readMaterializedViewQuery(quoted).Build()
			assert.Equal(t, map[string]string{"schema": `Odd"Schema`, "name": `My"Summary`}, parameters)
			return sql, err
		}},
		{"read_refresh", func() (string, error) {
			sql, parameters, err := readMaterializedViewRefreshQuery(quoted).Build()
			assert.Equal(t, map[string]string{"database": "admin", "schema": `Odd"Schema`, "name": `My"Summary`}, parameters)
			return sql, err
		}},
	})
}

// TestMaterializedViewAlterCoverage keeps an update step for every in-place materialized view attribute.
func TestMaterializedViewAlterCoverage(t *testing.T) {
	// The diststyle step also renders distkey, because Redshift alters the style and key in one clause.
	assertAlterCoverage(t, newMaterializedViewResource(), materializedViewAlterSteps, "distkey")
}

// TestMaterializedViewSortKeySkipsUnknownColumns keeps the configured order and leaves unknown columns, which
// only occur before apply, to the later validation.
func TestMaterializedViewSortKeySkipsUnknownColumns(t *testing.T) {
	columns, err := materializedViewSortKey(types.ListValueMust(types.StringType, []attr.Value{types.StringValue("z"), types.StringUnknown(), types.StringValue("a")}))
	require.NoError(t, err)
	assert.Equal(t, []string{"z", "a"}, columns)
	columns, err = materializedViewSortKey(types.ListUnknown(types.StringType))
	require.NoError(t, err)
	assert.Nil(t, columns)
}

// TestMaterializedViewFlag accepts the documented t/f flags and the 1/0 of the sample output only.
func TestMaterializedViewFlag(t *testing.T) {
	for value, expected := range map[string]bool{"t": true, "T": true, "1": true, "true": true, "f": false, "F": false, "0": false, "false": false} {
		flag, err := materializedViewFlag(value)
		require.NoError(t, err, value)
		assert.Equal(t, expected, flag, value)
	}
	for _, value := range []string{"", "u", "yes"} {
		_, err := materializedViewFlag(value)
		assert.Error(t, err, value)
	}
}
