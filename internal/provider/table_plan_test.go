package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTablePlanModifiers derives omitted styles and column values from the configuration and prior state.
func TestTablePlanModifiers(t *testing.T) {
	ctx := context.Background()
	r := newTableResource()
	config := func(change func(*tableModel)) tfsdk.Config {
		return tfsdk.Config(testState(t, r, tableWith(tableEventsModel(), change)))
	}
	style := func(source string, configured tfsdk.Config) types.String {
		block := "distribution"
		if source == "columns" {
			block = "sort_key"
		}
		resp := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
		tableDerivedStyle{source: source}.PlanModifyString(ctx, planmodifier.StringRequest{Path: path.Root(block).AtName("style"), Config: configured, ConfigValue: types.StringNull()}, &resp)
		return resp.PlanValue
	}
	assert.Equal(t, types.StringValue("KEY"), style("key", config(func(*tableModel) {})))
	assert.Equal(t, types.StringValue("AUTO"), style("key", config(func(m *tableModel) { m.Distribution = tableTestDistribution("", "") })))
	assert.True(t, style("key", config(func(m *tableModel) {
		m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "key": types.StringUnknown()})
	})).IsUnknown())
	assert.Equal(t, types.StringValue("COMPOUND"), style("columns", config(func(*tableModel) {})))
	assert.Equal(t, types.StringValue("AUTO"), style("columns", config(func(m *tableModel) { m.SortKey = tableTestSortKey("") })))
	assert.True(t, style("columns", config(func(m *tableModel) {
		m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "columns": types.ListUnknown(types.StringType)})
	})).IsUnknown())
	single := tfsdk.Config(testState(t, newSchemaResource(), schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringNull(), ID: types.StringNull()}))
	assert.True(t, style("columns", single).IsUnknown(), "unreadable configuration leaves the plan alone")
	assert.True(t, style("key", single).IsUnknown(), "unreadable configuration leaves the plan alone")
	configured := types.StringValue("EVEN")
	resp := planmodifier.StringResponse{PlanValue: configured}
	tableDerivedStyle{source: "key"}.PlanModifyString(ctx, planmodifier.StringRequest{ConfigValue: configured}, &resp)
	assert.Equal(t, configured, resp.PlanValue)
	assert.NotEmpty(t, tableDerivedStyle{source: "columns"}.MarkdownDescription(ctx))
	assert.NotEmpty(t, tableColumnDefaults{}.MarkdownDescription(ctx))

	unset := func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringUnknown(), types.BoolUnknown() }
	unconfigured := func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringNull(), types.BoolNull() }
	planned := tableTestColumns(
		tableTestColumn("code", "char(2)", unset),
		tableTestColumn("id", "bigint", unset, tableIdentity(1, 1, false)),
		tableTestColumn("note", "varchar(8)", unset),
		tableTestColumn("label", "varchar(64)", unset),
	)
	request := planmodifier.ListRequest{
		Config: config(func(m *tableModel) { m.PrimaryKey = tableTestKey("id", "code") }),
		ConfigValue: tableTestColumns(
			tableTestColumn("code", "char(2)", unconfigured),
			tableTestColumn("id", "bigint", unconfigured, tableIdentity(1, 1, false)),
			tableTestColumn("note", "varchar(8)", unconfigured),
			tableTestColumn("label", "varchar(64)", unconfigured),
		),
		PlanValue:  planned,
		StateValue: tableEventsModel().Column,
	}
	columnsResp := planmodifier.ListResponse{PlanValue: planned}
	tableColumnDefaults{}.PlanModifyList(ctx, request, &columnsResp)
	columns := tableTestColumnsOf(t, columnsResp.PlanValue)
	assert.Equal(t, []types.String{types.StringUnknown(), types.StringValue("AZ64"), types.StringUnknown(), types.StringValue("LZO")},
		[]types.String{columns[0].Encoding, columns[1].Encoding, columns[2].Encoding, columns[3].Encoding}, "existing columns keep their encoding, matched by name")
	assert.Equal(t, []types.Bool{types.BoolValue(false), types.BoolValue(false), types.BoolValue(true), types.BoolValue(true)},
		[]types.Bool{columns[0].Nullable, columns[1].Nullable, columns[2].Nullable, columns[3].Nullable}, "identity and key columns are NOT NULL")

	t.Run("sort key change", func(t *testing.T) {
		stored := func(name, dataType, encoding string) tableColumnModel {
			return tableTestColumn(name, dataType, tableEncoded(encoding), tableNullable(true))
		}
		prior := tableWith(tableEventsModel(), func(m *tableModel) {
			m.Column = tableTestColumns(stored("id", "bigint", "AZ64"), stored("label", "varchar(64)", "LZO"), stored("note", "varchar(8)", "LZO"))
		})
		columns := func(option func(*tableColumnModel)) types.List {
			return tableTestColumns(tableTestColumn("id", "bigint", option), tableTestColumn("label", "varchar(64)", option), tableTestColumn("note", "varchar(8)", option))
		}
		encodings := func(prior tableModel, change func(*tableModel)) []types.String {
			configured := tableWith(prior, func(m *tableModel) {
				m.Column = columns(unconfigured)
				change(m)
			})
			plannedColumns := tableTestColumnsOf(t, configured.Column)
			for i := range plannedColumns {
				unset(&plannedColumns[i])
			}
			plan := tableTestColumns(plannedColumns...)
			resp := planmodifier.ListResponse{PlanValue: plan}
			tableColumnDefaults{}.PlanModifyList(ctx, planmodifier.ListRequest{
				Config: tfsdk.Config(testState(t, r, configured)), State: testState(t, r, prior),
				ConfigValue: configured.Column, PlanValue: plan, StateValue: prior.Column,
			}, &resp)
			var encodings []types.String
			for _, column := range tableTestColumnsOf(t, resp.PlanValue) {
				encodings = append(encodings, column.Encoding)
			}
			return encodings
		}
		assert.Equal(t, []types.String{types.StringValue("AZ64"), types.StringValue("LZO"), types.StringValue("LZO")}, encodings(prior, func(*tableModel) {}),
			"an unchanged sort key keeps every encoding")
		assert.Equal(t, []types.String{types.StringUnknown(), types.StringValue("LZO"), types.StringUnknown()}, encodings(prior, func(m *tableModel) { m.SortKey = tableTestSortKey("", "note", "id") }),
			"ALTER SORTKEY may re-encode the columns of the old and new key")
		assert.Equal(t, []types.String{types.StringUnknown(), types.StringUnknown(), types.StringUnknown()}, encodings(prior, func(m *tableModel) {
			m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "columns": types.ListUnknown(types.StringType)})
		}), "an unknown sort key may re-encode any column")
		auto := tableWith(prior, tableAuto("", "label", "note"))
		assert.Equal(t, []types.String{types.StringValue("AZ64"), types.StringValue("LZO"), types.StringValue("LZO")}, encodings(auto, func(*tableModel) {}),
			"an unchanged AUTO sort key keeps every encoding")
		assert.Equal(t, []types.String{types.StringValue("AZ64"), types.StringUnknown()}, encodings(auto, func(m *tableModel) {
			m.Column = tableTestColumns(tableTestColumn("id", "bigint", unconfigured), tableTestColumn("label", "varchar(64)", unconfigured))
		}), "dropping a column of an AUTO key removes the key first, which may re-encode the rest of it")
	})

	unknownResp := planmodifier.ListResponse{PlanValue: planned}
	request.ConfigValue = types.ListUnknown(planned.ElementType(ctx))
	tableColumnDefaults{}.PlanModifyList(ctx, request, &unknownResp)
	assert.Equal(t, planned, unknownResp.PlanValue, "unknown configuration is left for apply")
}

// TestTableModifyPlanEffective plans a known effective layout for explicit layouts, keeps the recorded one for a plan
// that changes nothing else, and leaves an automatic or not yet known one to apply.
func TestTableModifyPlanEffective(t *testing.T) {
	events := tableEventsModel()
	note := tableTestColumn("note", "varchar(32)", tableEncoded("LZO"), tableNullable(true))
	withNote := tableWith(events, func(m *tableModel) { m.Column = tableTestColumns(append(tableTestColumnsOf(t, m.Column), note)...) })
	auto := tableWith(withNote, tableAuto("note", "note", "id"))
	unknownDistribution := types.ObjectUnknown(tableEffectiveDistributionAttributeTypes)
	unknownSortKey := types.ObjectUnknown(tableEffectiveSortKeyAttributeTypes)
	unknownStyles := func(m *tableModel) {
		m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "key": types.StringValue("id")})
		m.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "columns": tableStringListValue([]string{"id"})})
	}
	for name, test := range map[string]struct {
		plan         tableModel
		distribution types.Object
		sortKey      types.Object
	}{
		"explicit": {events, tableEffectiveDistributionValue("KEY", "id", false), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, false)},
		"even and none": {tableWith(events, func(m *tableModel) {
			m.Distribution, m.SortKey = tableTestDistribution("EVEN", ""), tableTestSortKey("NONE")
		}), tableEffectiveDistributionValue("EVEN", "", false), tableEffectiveSortKeyValue("NONE", nil, false)},
		"auto":          {auto, unknownDistribution, unknownSortKey},
		"unknown style": {tableWith(events, unknownStyles), unknownDistribution, unknownSortKey},
		"unknown blocks": {tableWith(events, func(m *tableModel) {
			m.Distribution, m.SortKey = types.ObjectUnknown(tableDistributionAttributeTypes), types.ObjectUnknown(tableSortKeyAttributeTypes)
		}), unknownDistribution, unknownSortKey},
		"invalid plan":          {tableWith(events, func(m *tableModel) { m.Distribution = tableTestDistribution("", "missing") }), unknownDistribution, unknownSortKey},
		"explicit style object": {tableWith(events, func(m *tableModel) { m.Distribution = tableTestDistribution("ALL", "") }), tableEffectiveDistributionValue("ALL", "", false), tableEffectiveSortKeyValue("COMPOUND", []string{"id"}, false)},
	} {
		t.Run(name, func(t *testing.T) {
			distribution, sortKey := tablePlannedEffective(test.plan)
			assert.Equal(t, test.distribution, distribution)
			assert.Equal(t, test.sortKey, sortKey)
		})
	}

	ctx := context.Background()
	r := newTableResource().(*tableResource)
	// modifyPlan runs ModifyPlan as Terraform calls it on an update: the framework has marked the computed effective
	// values unknown whenever the plan differs from the state.
	modifyPlan := func(t *testing.T, prior, plan tableModel) tableModel {
		t.Helper()
		planned := testState(t, r, plan)
		resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan(planned)}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan(planned), State: testState(t, r, prior)}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		var result tableResourceModel
		require.False(t, resp.Plan.Get(ctx, &result).HasError())
		return result.tableModel
	}
	marked := func(m *tableModel) { m.EffectiveDistribution, m.EffectiveSortKey = unknownDistribution, unknownSortKey }
	unrecorded := tableWith(auto, func(m *tableModel) {
		m.EffectiveDistribution, m.EffectiveSortKey = types.ObjectNull(tableEffectiveDistributionAttributeTypes), types.ObjectNull(tableEffectiveSortKeyAttributeTypes)
	})
	for name, test := range map[string]struct {
		prior        tableModel
		plan         tableModel
		distribution types.Object
		sortKey      types.Object
	}{
		"auto unchanged":               {auto, auto, auto.EffectiveDistribution, auto.EffectiveSortKey},
		"auto marked":                  {auto, tableWith(auto, marked), auto.EffectiveDistribution, auto.EffectiveSortKey},
		"auto unrecorded":              {unrecorded, unrecorded, unrecorded.EffectiveDistribution, unrecorded.EffectiveSortKey},
		"auto owner change":            {auto, tableWith(auto, func(m *tableModel) { marked(m); m.Owner = types.StringValue("analyst") }), unknownDistribution, unknownSortKey},
		"auto drops chosen key column": {auto, tableWith(auto, func(m *tableModel) { marked(m); m.Column = events.Column }), unknownDistribution, unknownSortKey},
		"explicit to auto":             {events, tableWith(events, func(m *tableModel) { tableAuto("", "")(m); marked(m) }), unknownDistribution, unknownSortKey},
		"auto to explicit":             {auto, tableWith(withNote, marked), events.EffectiveDistribution, events.EffectiveSortKey},
		"explicit unchanged":           {events, events, events.EffectiveDistribution, events.EffectiveSortKey},
		"unknown style":                {events, tableWith(events, func(m *tableModel) { marked(m); unknownStyles(m) }), unknownDistribution, unknownSortKey},
	} {
		t.Run(name, func(t *testing.T) {
			planned := modifyPlan(t, test.prior, test.plan)
			assert.Equal(t, test.distribution, planned.EffectiveDistribution)
			assert.Equal(t, test.sortKey, planned.EffectiveSortKey)
		})
	}
	t.Run("create", func(t *testing.T) {
		planned := testState(t, r, tableWith(auto, marked))
		resp := resource.ModifyPlanResponse{Plan: tfsdk.Plan(planned)}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan(planned), State: emptyState(t, r)}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		var result tableResourceModel
		require.False(t, resp.Plan.Get(ctx, &result).HasError())
		assert.Equal(t, unknownDistribution, result.EffectiveDistribution)
	})
	destroy := resource.ModifyPlanResponse{Plan: tfsdk.Plan(emptyState(t, r))}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan(emptyState(t, r)), State: testState(t, r, auto)}, &destroy)
	assert.False(t, destroy.Diagnostics.HasError(), "a destroy plan is left alone")
}

// TestTableSortKeyBlock derives the declared sort key of a block, and refuses values that are not known yet.
func TestTableSortKeyBlock(t *testing.T) {
	sortKey := func(style types.String, columns types.List) types.Object {
		return types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": style, "columns": columns})
	}
	for name, test := range map[string]struct {
		block   types.Object
		style   string
		columns []string
		known   bool
	}{
		"omitted":         {types.ObjectNull(tableSortKeyAttributeTypes), "AUTO", nil, true},
		"unknown":         {types.ObjectUnknown(tableSortKeyAttributeTypes), "", nil, false},
		"unknown style":   {sortKey(types.StringUnknown(), types.ListNull(types.StringType)), "", nil, false},
		"unknown columns": {sortKey(types.StringNull(), types.ListUnknown(types.StringType)), "", nil, false},
		"columns only":    {tableTestSortKey("", "a", "b"), "COMPOUND", []string{"a", "b"}, true},
		"empty block":     {tableTestSortKey(""), "AUTO", nil, true},
		"explicit":        {tableTestSortKey("INTERLEAVED", "a"), "INTERLEAVED", []string{"a"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			style, columns, known := tableSortKeyBlock(test.block)
			assert.Equal(t, test.known, known)
			assert.Equal(t, test.style, style)
			assert.Equal(t, test.columns, columns)
		})
	}
	assert.Nil(t, tableEffectiveSortColumns(types.ObjectUnknown(tableEffectiveSortKeyAttributeTypes)))
	assert.Nil(t, tableEffectiveSortColumns(types.ObjectValueMust(tableEffectiveSortKeyAttributeTypes, map[string]attr.Value{
		"style": types.StringValue("COMPOUND"), "columns": types.ListUnknown(types.StringType), "auto": types.BoolValue(true),
	})), "columns that are not known yet are no key")
	assert.Equal(t, []string{"a"}, tableEffectiveSortColumns(tableEffectiveSortKeyValue("COMPOUND", []string{"a"}, true)))
}

// TestTableKnownState writes unknown computed values as nulls before verification.
func TestTableKnownState(t *testing.T) {
	model := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Owner = types.StringUnknown()
		m.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringUnknown(), "key": types.StringNull()})
		m.EffectiveDistribution = types.ObjectUnknown(tableEffectiveDistributionAttributeTypes)
		m.Column = tableTestColumns(tableTestColumn("a", "int", func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringUnknown(), types.BoolUnknown() }))
	})
	known := tableKnownState(model)
	assert.True(t, known.Owner.IsNull())
	assert.Equal(t, tableTestDistribution("", ""), known.Distribution)
	assert.True(t, known.EffectiveDistribution.IsNull())
	assert.Equal(t, model.EffectiveSortKey, known.EffectiveSortKey)
	columns := tableTestColumnsOf(t, known.Column)
	assert.True(t, columns[0].Encoding.IsNull())
	assert.True(t, columns[0].Nullable.IsNull())
	unknownColumns := tableWith(model, func(m *tableModel) { m.Column = types.ListUnknown(m.Column.ElementType(context.Background())) })
	assert.True(t, tableKnownState(unknownColumns).Column.IsUnknown())
}
