package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rlsPolicyColumnsValue builds a WITH column list from name/type pairs.
func rlsPolicyColumnsValue(pairs ...string) types.List {
	elements := make([]attr.Value, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		elements = append(elements, types.ObjectValueMust(rlsPolicyColumnType.AttrTypes, map[string]attr.Value{"name": types.StringValue(pairs[i]), "type": types.StringValue(pairs[i+1])}))
	}
	return types.ListValueMust(rlsPolicyColumnType, elements)
}

// rlsPolicySample is a representative policy with one WITH column.
func rlsPolicySample() rlsPolicyModel {
	return rlsPolicyModel{
		Database: types.StringValue("analytics"), Name: types.StringValue("region_filter"),
		Columns: rlsPolicyColumnsValue("region", "VARCHAR(64)"), Alias: types.StringNull(),
		Predicate: types.StringValue("region = current_user"), DefinitionFingerprint: types.StringNull(),
	}
}

// TestRlsPolicySQL pins the policy statements and catalog reads, including quoting edge cases.
func TestRlsPolicySQL(t *testing.T) {
	with := func(change func(*rlsPolicyModel)) rlsPolicyModel {
		data := rlsPolicySample()
		change(&data)
		return data
	}
	create := func(data rlsPolicyModel) func() (string, error) {
		return func() (string, error) { return createRlsPolicyStatement(data) }
	}
	alter := func(prev, plan rlsPolicyModel) func() ([]string, error) {
		return func() ([]string, error) { return alterRlsPolicyStatements(prev, plan) }
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	quoted := with(func(d *rlsPolicyModel) {
		d.Name = types.StringValue(`Odd"Policy`)
		d.Columns = rlsPolicyColumnsValue(`Odd"Region`, "varchar", "tenant_id", "int4")
		d.Alias = types.StringValue(`T"Alias`)
		d.Predicate = types.StringValue(`"T""Alias"."Odd""Region" = 'O''Brien \ path' AND tenant_id > 0`)
	})
	changed := with(func(d *rlsPolicyModel) { d.Predicate = types.StringValue("region IN ('eu', 'us')") })
	checkSQL(t, "rls_policy", []sqlCase{
		{"create", create(rlsPolicySample())},
		{"create_without_columns", create(with(func(d *rlsPolicyModel) {
			d.Columns = types.ListNull(rlsPolicyColumnType)
			d.Predicate = types.StringValue("current_user = 'auditor'")
		}))},
		{"create_alias", create(with(func(d *rlsPolicyModel) {
			d.Alias = types.StringValue("t")
			d.Predicate = types.StringValue("t.region = current_user")
		}))},
		{"create_quoted", create(quoted)},
		{"create_trailing_comment", create(with(func(d *rlsPolicyModel) { d.Predicate = types.StringValue("region = current_user -- own rows") }))},
		{"create_alias_without_columns", create(with(func(d *rlsPolicyModel) {
			d.Columns = types.ListNull(rlsPolicyColumnType)
			d.Alias = types.StringValue("t")
		}))},
		{"create_invalid_type", create(with(func(d *rlsPolicyModel) { d.Columns = rlsPolicyColumnsValue("region", "VARCHAR(64); DROP TABLE x") }))},
		{"create_empty_column_name", create(with(func(d *rlsPolicyModel) { d.Columns = rlsPolicyColumnsValue("", "INTEGER") }))},
		{"create_statement_in_predicate", create(with(func(d *rlsPolicyModel) { d.Predicate = types.StringValue("true; DROP RLS POLICY other") }))},
		{"create_unterminated_predicate", create(with(func(d *rlsPolicyModel) { d.Predicate = types.StringValue("region = 'eu") }))},
		{"create_empty_name", create(with(func(d *rlsPolicyModel) { d.Name = types.StringValue("") }))},
		{"alter_predicate", alter(rlsPolicySample(), changed)},
		{"alter_quoted", alter(rlsPolicySample(), quoted)},
		{"alter_unchanged", alter(rlsPolicySample(), rlsPolicySample())},
		{"alter_invalid_predicate", alter(rlsPolicySample(), with(func(d *rlsPolicyModel) { d.Predicate = types.StringValue("(region") }))},
		{"drop", func() string { return dropRlsPolicyStatement(rlsPolicySample()) }},
		{"drop_quoted", func() string { return dropRlsPolicyStatement(quoted) }},
		{"read", built(readRlsPolicyQuery(quoted))},
		{"read_empty_name", built(readRlsPolicyQuery(with(func(d *rlsPolicyModel) { d.Name = types.StringValue("") })))},
		{"list", built(listRlsPoliciesQuery(`Odd"Database`))},
	})
}

// TestRlsPolicyColumnsMatch compares configured columns with polatts in canonical form.
func TestRlsPolicyColumnsMatch(t *testing.T) {
	configured, err := rlsPolicyColumns(rlsPolicyColumnsValue("Region", "varchar", "tenant", "int4"))
	require.NoError(t, err)
	catalog, err := parseRlsPolicyColumns(`[{"colname":"region","type":"character varying(256)"},{"colname":"tenant","type":"integer"}]`)
	require.NoError(t, err)
	assert.True(t, rlsPolicyColumnsMatch(configured, catalog))
	assert.False(t, rlsPolicyColumnsMatch(configured[:1], catalog), "a missing column differs")
	assert.False(t, rlsPolicyColumnsMatch(configured, []rlsPolicyCatalogColumn{catalog[0], {Name: "tenant", Type: "bigint"}}), "a different type differs")
	assert.False(t, rlsPolicyColumnsMatch(configured, []rlsPolicyCatalogColumn{catalog[0], {Name: "tenant", Type: "unknown type"}}), "an unparsable catalog type differs")
	assert.False(t, rlsPolicyColumnsMatch(configured, []rlsPolicyCatalogColumn{catalog[1], catalog[0]}), "order matters")
	for _, empty := range []string{"", " ", "[]"} {
		columns, err := parseRlsPolicyColumns(empty)
		require.NoError(t, err)
		assert.Empty(t, columns)
		assert.True(t, rlsPolicyColumnsList(columns).IsNull())
	}
	_, err = parseRlsPolicyColumns("{")
	require.Error(t, err)
	none, err := rlsPolicyColumns(types.ListUnknown(rlsPolicyColumnType))
	require.NoError(t, err)
	assert.Empty(t, none)
	_, err = rlsPolicyColumns(types.ListValueMust(rlsPolicyColumnType, []attr.Value{types.ObjectNull(rlsPolicyColumnType.AttrTypes)}))
	require.Error(t, err)
}
