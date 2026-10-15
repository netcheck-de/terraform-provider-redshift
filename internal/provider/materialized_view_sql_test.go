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
		Query: types.StringValue(fakeMaterializedQuery), Backup: types.BoolNull(), Distribution: types.ObjectNull(materializedViewDistributionTypes),
		SortKey: types.ObjectNull(materializedViewSortKeyTypes), AutoRefresh: types.BoolValue(true), Owner: types.StringValue(fakeViewOwner), DefinitionFingerprint: types.StringNull(),
	}
}

// materializedViewDistributionTypes and materializedViewSortKeyTypes are the attribute types of the distribution and
// sort_key blocks.
var (
	materializedViewDistributionTypes = map[string]attr.Type{"style": types.StringType, "key": types.StringType}
	materializedViewSortKeyTypes      = map[string]attr.Type{"columns": types.ListType{ElemType: types.StringType}}
)

// materializedViewDistributionBlock builds a distribution block; an empty style or key is left unset.
func materializedViewDistributionBlock(style, key string) types.Object {
	optional := func(value string) attr.Value {
		if value == "" {
			return types.StringNull()
		}
		return types.StringValue(value)
	}
	return types.ObjectValueMust(materializedViewDistributionTypes, map[string]attr.Value{"style": optional(style), "key": optional(key)})
}

// materializedViewSortKeyBlock builds a sort_key block with the columns in order.
func materializedViewSortKeyBlock(columns ...string) types.Object {
	elements := make([]attr.Value, len(columns))
	for i, column := range columns {
		elements[i] = types.StringValue(column)
	}
	return types.ObjectValueMust(materializedViewSortKeyTypes, map[string]attr.Value{"columns": types.ListValueMust(types.StringType, elements)})
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
		data.Distribution, data.SortKey = materializedViewDistributionBlock("", `Label"Col`), materializedViewSortKeyBlock(`Label"Col`, "ID")
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
			data.Backup, data.Distribution = types.BoolValue(false), materializedViewDistributionBlock("KEY", "label")
			data.SortKey = materializedViewSortKeyBlock("label", "sales")
		}))},
		{"create_backup_yes_diststyle_all", create(with(func(data *materializedViewModel) {
			data.Backup, data.Distribution = types.BoolValue(true), materializedViewDistributionBlock("all", "")
		}))},
		{"create_distkey_without_style", create(with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("", "label") }))},
		{"create_quoted", create(quoted)},
		{"create_distkey_with_even", create(with(func(data *materializedViewModel) {
			data.Distribution = materializedViewDistributionBlock("EVEN", "label")
		}))},
		{"create_key_without_distkey", create(with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("KEY", "") }))},
		{"create_auto_diststyle", create(with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("AUTO", "") }))},
		{"create_empty_distribution", create(with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("", "") }))},
		{"create_empty_sort_key", create(with(func(data *materializedViewModel) {
			data.SortKey = types.ObjectValueMust(materializedViewSortKeyTypes, map[string]attr.Value{"columns": types.ListNull(types.StringType)})
		}))},
		{"create_empty_sortkey_column", create(with(func(data *materializedViewModel) { data.SortKey = materializedViewSortKeyBlock("label", " ") }))},
		{"create_semicolon", create(with(func(data *materializedViewModel) { data.Query = types.StringValue("SELECT 1; SELECT 2") }))},
		{"create_empty_name", create(with(func(data *materializedViewModel) { data.Name = types.StringValue("") }))},
		{"alter_unchanged", alter(testMaterializedViewModel(), testMaterializedViewModel())},
		{"alter_auto_refresh_off", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.AutoRefresh = types.BoolValue(false) }))},
		{"alter_quoted_all_in_place_options", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.AutoRefresh = quoted.Schema, quoted.Name, types.BoolValue(false)
		}), quoted)},
		{"alter_adopts_imported_options", alter(with(func(data *materializedViewModel) { data.Query = types.StringNull() }), with(func(data *materializedViewModel) {
			data.Backup, data.SortKey = types.BoolValue(false), materializedViewSortKeyBlock("label")
		}))},
		{"alter_diststyle_all", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("ALL", "") }))},
		{"alter_distkey_quoted", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.Distribution = quoted.Schema, quoted.Name, materializedViewDistributionBlock("ALL", "")
		}), with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.Distribution = quoted.Schema, quoted.Name, quoted.Distribution
		}))},
		{"alter_distribution_removed", alter(with(func(data *materializedViewModel) {
			data.Distribution = materializedViewDistributionBlock("KEY", "label")
		}), testMaterializedViewModel())},
		{"alter_explicit_key_style_unchanged", alter(with(func(data *materializedViewModel) { data.Distribution = materializedViewDistributionBlock("", "label") }), with(func(data *materializedViewModel) {
			data.Distribution = materializedViewDistributionBlock("KEY", "label")
		}))},
		{"alter_distribution_unknown", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) {
			data.Distribution, data.SortKey = types.ObjectUnknown(materializedViewDistributionTypes), types.ObjectUnknown(materializedViewSortKeyTypes)
		}))},
		{"alter_distkey_with_all", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) {
			data.Distribution = materializedViewDistributionBlock("ALL", "label")
		}))},
		{"alter_sortkey_quoted", alter(with(func(data *materializedViewModel) {
			data.Schema, data.Name = quoted.Schema, quoted.Name
		}), with(func(data *materializedViewModel) {
			data.Schema, data.Name, data.SortKey = quoted.Schema, quoted.Name, quoted.SortKey
		}))},
		{"alter_sortkey_removed", alter(with(func(data *materializedViewModel) { data.SortKey = materializedViewSortKeyBlock("label") }), testMaterializedViewModel())},
		{"alter_storage_then_owner", alter(testMaterializedViewModel(), with(func(data *materializedViewModel) {
			data.Distribution, data.SortKey = materializedViewDistributionBlock("EVEN", ""), materializedViewSortKeyBlock("label")
			data.AutoRefresh, data.Owner = types.BoolValue(false), types.StringValue("reporter")
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

// TestMaterializedViewAlterCoverage keeps an update step for every in-place materialized view attribute and block.
func TestMaterializedViewAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newMaterializedViewResource(), materializedViewAlterSteps)
}

// TestMaterializedViewSortKeySkipsUnknownColumns keeps the configured order and leaves unknown columns, which
// only occur before apply, to the later validation.
func TestMaterializedViewSortKeySkipsUnknownColumns(t *testing.T) {
	data := testMaterializedViewModel()
	assert.True(t, materializedViewSortKeyColumns(data).IsNull(), "an absent block has no columns")
	data.SortKey = types.ObjectUnknown(materializedViewSortKeyTypes)
	assert.True(t, materializedViewSortKeyColumns(data).IsUnknown(), "an unknown block has unknown columns")
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
