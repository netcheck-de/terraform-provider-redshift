package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageTableMissingDatabase(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []bool{false, true} {
		client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			if failure {
				return nil, errors.New("database lookup failed")
			}
			return nil, nil
		})
		table := &tableResource{}
		configureTestResource(t, table, client)
		model := tableMinimalModel()
		model.Database = types.StringValue("missing")
		found, _, err := table.read(ctx, &model)
		assert.False(t, found)
		if failure {
			require.ErrorContains(t, err, "database lookup failed")
		} else {
			require.NoError(t, err)
		}
		security := &tableSecurityResource{}
		configureTestResource(t, security, client)
		securityModel := tableSecurityModel{Database: types.StringValue("missing"), ID: types.StringNull()}
		found, err = security.read(ctx, &securityModel)
		assert.False(t, found)
		if failure {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		external := externalTableTestResource(t, client)
		externalModel := externalTableTestModel()
		externalModel.Database = types.StringValue("missing")
		catalog, err := external.fetch(ctx, externalModel)
		assert.Nil(t, catalog)
		if failure {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		partition := &externalPartitionResource{}
		configureTestResource(t, partition, client)
		partitionModel := externalPartitionTestModel()
		partitionModel.Database = types.StringValue("missing")
		keys, err := partition.partitionKeys(ctx, partitionModel)
		assert.Nil(t, keys)
		if failure {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestCoverageTableConfiguredNames(t *testing.T) {
	ctx := context.Background()
	r := &tableResource{}
	for _, test := range []struct {
		name    string
		columns types.List
		known   bool
		want    map[string]bool
	}{
		{"unknown list", types.ListUnknown(types.ObjectType{AttrTypes: tableColumnAttributeTypes}), false, nil},
		{"unknown name", tableTestColumns(tableTestColumn("id", "integer", func(c *tableColumnModel) { c.Name = types.StringUnknown() })), false, nil},
		{"rename endpoints", tableTestColumns(tableTestColumn("new", "integer", func(c *tableColumnModel) { c.RenamedFrom = types.StringValue("old") })), true, map[string]bool{"new": true, "old": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := tableMinimalModel()
			model.Column = test.columns
			config := tfsdk.Config(testState(t, r, model))
			names, known := tableConfiguredColumnNames(ctx, config)
			assert.Equal(t, test.known, known)
			assert.Equal(t, test.want, names)
		})
	}
	assert.True(t, tableStyleUnknown(types.ObjectUnknown(tableSortKeyAttributeTypes)))
	assert.True(t, (tableSpec{primaryKey: []string{"id"}}).constrained("id"))
	model := tableMinimalModel()
	model.Backup = types.StringUnknown()
	observed, _, err := tableReconcile(model, tableCatalog{owner: "admin", distStyle: "AUTO", columns: []tableCatalogColumn{{name: "payload", dataType: "SUPER", encoding: "RAW"}}})
	require.NoError(t, err)
	assert.True(t, observed.Backup.IsNull())
	// The lookup's computed layout starts unknown; AUTO cannot yet identify specific columns.
	model.Column = types.ListUnknown(types.ObjectType{AttrTypes: tableColumnAttributeTypes})
	model.SortKey = types.ObjectNull(tableSortKeyAttributeTypes)
	stateModel := tableMinimalModel()
	stateModel.EffectiveSortKey = tableEffectiveSortKeyValue("COMPOUND", []string{"payload"}, true)
	_, all := tableSortKeyMoves(ctx, tfsdk.Config(testState(t, r, model)), testState(t, r, stateModel))
	assert.True(t, all)
	model.Column = tableTestColumns(tableTestColumn("new", "integer", func(c *tableColumnModel) { c.RenamedFrom = types.StringValue("old") }))
	stateModel.EffectiveSortKey = tableEffectiveSortKeyValue("COMPOUND", []string{"old", "dropped"}, true)
	moved, all := tableSortKeyMoves(ctx, tfsdk.Config(testState(t, r, model)), testState(t, r, stateModel))
	assert.False(t, all)
	assert.Equal(t, map[string]bool{"old": true, "new": true, "dropped": true}, moved)
}

func TestCoverageTableReconciliationFailure(t *testing.T) {
	c := fullCatalog()
	r := &tableResource{}
	configureTestResource(t, r, queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, connection, sql, parameters)
		if strings.Contains(sql, "c.reldiststyle AS diststyle") {
			for _, row := range rows {
				row["diststyle"] = "1"
			}
		}
		if strings.Contains(sql, "svv_redshift_columns") {
			for _, row := range rows {
				row["distkey"] = "false"
			}
		}
		return rows, err
	}))
	model := tableEventsModel()
	found, _, err := r.read(context.Background(), &model)
	assert.False(t, found)
	require.ErrorContains(t, err, "without a distribution key")
}

func TestCoverageTableRejectInvalidUpdate(t *testing.T) {
	c := fullCatalog()
	r := &tableResource{}
	configureTestResource(t, r, c)
	prior := tableEventsModel()
	plan := prior
	plan.Schema = types.StringValue("")
	_, diagnostics := applyOperation(t, r, "update", prior, plan, nil)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Invalid table", diagnostics.Errors()[0].Summary())
	assert.Empty(t, c.writes)
}

func TestCoverageTableColumnDefaultsUnknownBlock(t *testing.T) {
	planned := types.ListValueMust(types.ObjectType{AttrTypes: tableColumnAttributeTypes}, []attr.Value{types.ObjectUnknown(tableColumnAttributeTypes)})
	response := planmodifier.ListResponse{PlanValue: planned}
	(tableColumnDefaults{}).PlanModifyList(context.Background(), planmodifier.ListRequest{PlanValue: planned, ConfigValue: planned}, &response)
	assert.Equal(t, planned, response.PlanValue)
	assert.Empty(t, response.Diagnostics)
}
