package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
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
		resp := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
		tableDerivedStyle{source: source}.PlanModifyString(ctx, planmodifier.StringRequest{Config: configured, ConfigValue: types.StringNull()}, &resp)
		return resp.PlanValue
	}
	assert.Equal(t, types.StringValue("KEY"), style("distkey", config(func(*tableModel) {})))
	assert.Equal(t, types.StringValue("AUTO"), style("distkey", config(func(m *tableModel) { m.DistKey = types.StringNull() })))
	assert.True(t, style("distkey", config(func(m *tableModel) { m.DistKey = types.StringUnknown() })).IsUnknown())
	assert.Equal(t, types.StringValue("COMPOUND"), style("sortkey", config(func(*tableModel) {})))
	assert.Equal(t, types.StringValue("AUTO"), style("sortkey", config(func(m *tableModel) { m.SortKey = tableTestList() })))
	assert.True(t, style("sortkey", config(func(m *tableModel) { m.SortKey = types.ListUnknown(types.StringType) })).IsUnknown())
	single := tfsdk.Config(testState(t, newSchemaResource(), schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringNull(), ID: types.StringNull()}))
	assert.True(t, style("sortkey", single).IsUnknown(), "unreadable configuration leaves the plan alone")
	assert.True(t, style("distkey", single).IsUnknown(), "unreadable configuration leaves the plan alone")
	configured := types.StringValue("EVEN")
	resp := planmodifier.StringResponse{PlanValue: configured}
	tableDerivedStyle{source: "distkey"}.PlanModifyString(ctx, planmodifier.StringRequest{ConfigValue: configured}, &resp)
	assert.Equal(t, configured, resp.PlanValue)
	assert.NotEmpty(t, tableDerivedStyle{source: "sortkey"}.MarkdownDescription(ctx))
	assert.NotEmpty(t, tableColumnDefaults{}.MarkdownDescription(ctx))

	unset := func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringUnknown(), types.BoolUnknown() }
	unconfigured := func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringNull(), types.BoolNull() }
	planned := tableTestColumns(
		tableTestColumn("id", "bigint", unset, tableIdentity(1, 1, false)),
		tableTestColumn("label", "varchar(64)", unset),
		tableTestColumn("code", "char(2)", unset),
		tableTestColumn("note", "varchar(8)", unset),
	)
	request := planmodifier.ListRequest{
		Config: config(func(m *tableModel) { m.PrimaryKey = tableTestList("id", "code") }),
		ConfigValue: tableTestColumns(
			tableTestColumn("id", "bigint", unconfigured, tableIdentity(1, 1, false)),
			tableTestColumn("label", "varchar(64)", unconfigured),
			tableTestColumn("code", "char(2)", unconfigured),
			tableTestColumn("note", "varchar(8)", unconfigured),
		),
		PlanValue:  planned,
		StateValue: tableEventsModel().Columns,
	}
	columnsResp := planmodifier.ListResponse{PlanValue: planned}
	tableColumnDefaults{}.PlanModifyList(ctx, request, &columnsResp)
	var columns []tableColumnModel
	require.False(t, columnsResp.PlanValue.ElementsAs(ctx, &columns, false).HasError())
	assert.Equal(t, []types.String{types.StringValue("AZ64"), types.StringValue("LZO"), types.StringUnknown(), types.StringUnknown()},
		[]types.String{columns[0].Encoding, columns[1].Encoding, columns[2].Encoding, columns[3].Encoding}, "existing columns keep their encoding")
	assert.Equal(t, []types.Bool{types.BoolValue(false), types.BoolValue(true), types.BoolValue(false), types.BoolValue(true)},
		[]types.Bool{columns[0].Nullable, columns[1].Nullable, columns[2].Nullable, columns[3].Nullable}, "identity and key columns are NOT NULL")

	t.Run("sort key change", func(t *testing.T) {
		stored := func(name, dataType, encoding string) tableColumnModel {
			return tableTestColumn(name, dataType, tableEncoded(encoding), tableNullable(true))
		}
		prior := tableWith(tableEventsModel(), func(m *tableModel) {
			m.Columns = tableTestColumns(stored("id", "bigint", "AZ64"), stored("label", "varchar(64)", "LZO"), stored("note", "varchar(8)", "LZO"))
		})
		columns := func(option func(*tableColumnModel)) types.List {
			return tableTestColumns(tableTestColumn("id", "bigint", option), tableTestColumn("label", "varchar(64)", option), tableTestColumn("note", "varchar(8)", option))
		}
		encodings := func(sortKey types.List) []types.String {
			configured := tableWith(prior, func(m *tableModel) { m.Columns, m.SortKey = columns(unconfigured), sortKey })
			resp := planmodifier.ListResponse{PlanValue: columns(unset)}
			tableColumnDefaults{}.PlanModifyList(ctx, planmodifier.ListRequest{
				Config: tfsdk.Config(testState(t, r, configured)), State: testState(t, r, prior),
				ConfigValue: configured.Columns, PlanValue: columns(unset), StateValue: prior.Columns,
			}, &resp)
			var planned []tableColumnModel
			require.False(t, resp.PlanValue.ElementsAs(ctx, &planned, false).HasError())
			return []types.String{planned[0].Encoding, planned[1].Encoding, planned[2].Encoding}
		}
		assert.Equal(t, []types.String{types.StringValue("AZ64"), types.StringValue("LZO"), types.StringValue("LZO")}, encodings(tableTestList("id")),
			"an unchanged sort key keeps every encoding")
		assert.Equal(t, []types.String{types.StringUnknown(), types.StringValue("LZO"), types.StringUnknown()}, encodings(tableTestList("note", "id")),
			"ALTER SORTKEY may re-encode the columns of the old and new key")
		assert.Equal(t, []types.String{types.StringUnknown(), types.StringUnknown(), types.StringUnknown()}, encodings(types.ListUnknown(types.StringType)),
			"an unknown sort key may re-encode any column")
	})

	unknownResp := planmodifier.ListResponse{PlanValue: planned}
	request.ConfigValue = types.ListUnknown(planned.ElementType(ctx))
	tableColumnDefaults{}.PlanModifyList(ctx, request, &unknownResp)
	assert.Equal(t, planned, unknownResp.PlanValue, "unknown configuration is left for apply")
}

// TestTableKnownState writes unknown computed values as nulls before verification.
func TestTableKnownState(t *testing.T) {
	model := tableWith(tableEventsModel(), func(m *tableModel) {
		m.Owner, m.DistStyle = types.StringUnknown(), types.StringUnknown()
		m.Columns = tableTestColumns(tableTestColumn("a", "int", func(c *tableColumnModel) { c.Encoding, c.Nullable = types.StringUnknown(), types.BoolUnknown() }))
	})
	known := tableKnownState(model)
	assert.True(t, known.Owner.IsNull())
	assert.True(t, known.DistStyle.IsNull())
	var columns []tableColumnModel
	require.False(t, known.Columns.ElementsAs(context.Background(), &columns, false).HasError())
	assert.True(t, columns[0].Encoding.IsNull())
	assert.True(t, columns[0].Nullable.IsNull())
	unknownColumns := tableWith(model, func(m *tableModel) { m.Columns = types.ListUnknown(m.Columns.ElementType(context.Background())) })
	assert.True(t, tableKnownState(unknownColumns).Columns.IsUnknown())
}

// TestTableAutoSortKeyRead keeps SORTKEY AUTO for keys Redshift chose or kept, and reports a key SVV_TABLE_INFO
// shows as explicit.
func TestTableAutoSortKeyRead(t *testing.T) {
	auto := tableWith(tableEventsModel(), func(m *tableModel) {
		m.SortKeyStyle, m.SortKey = types.StringValue("AUTO"), types.ListNull(types.StringType)
	})
	for name, test := range map[string]struct {
		rows  []dataapi.Row
		style string
	}{
		"chosen by Redshift": {[]dataapi.Row{{"sortkey1": "AUTO(SORTKEY(id))"}}, "AUTO"},
		"empty table":        {nil, "AUTO"},
		"explicit":           {[]dataapi.Row{{"sortkey1": "id"}}, "COMPOUND"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			r := &tableResource{testResourceClient(queryFunc(func(ctx context.Context, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "SELECT sortkey1") {
					return test.rows, nil
				}
				return c.Query(ctx, connection, sql, parameters)
			}))}
			data := auto
			found, _, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, test.style, data.SortKeyStyle.ValueString())
		})
	}
}
