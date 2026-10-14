package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationDatabaseModel returns the "replica" destination of the fake integration with no settings configured.
func integrationDatabaseModel() databaseModel {
	return databaseModel{
		Name: types.StringValue(fakeIntegrationDatabase), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true),
		Owner: types.StringNull(), ConnectionLimit: types.Int64Null(), Collation: types.StringNull(), IsolationLevel: types.StringNull(),
		Integration: &databaseIntegrationModel{
			IntegrationID: types.StringValue(fakeIntegrationID), SourceDatabase: types.StringNull(), RefreshInterval: types.Int64Null(),
			HistoryMode: types.BoolNull(), AutoRemediation: types.BoolNull(), QueryAllStates: types.BoolNull(),
			AcceptInvalidChars: types.BoolNull(), TruncateColumns: types.BoolNull(),
		},
	}
}

// configuredIntegrationDatabase sets every integration setting and the local options a destination accepts.
func configuredIntegrationDatabase() databaseModel {
	data := integrationDatabaseModel()
	data.Owner, data.ConnectionLimit, data.IsolationLevel = types.StringValue(`Etl"Owner`), types.Int64Value(25), types.StringValue("SERIALIZABLE")
	integration := *data.Integration
	integration.SourceDatabase = types.StringValue(`it's"orders`)
	integration.RefreshInterval = types.Int64Value(600)
	integration.HistoryMode, integration.AutoRemediation = types.BoolValue(true), types.BoolValue(true)
	integration.QueryAllStates, integration.AcceptInvalidChars, integration.TruncateColumns = types.BoolValue(true), types.BoolValue(false), types.BoolValue(true)
	data.Integration = &integration
	return data
}

// TestDatabaseIntegrationSQL pins CREATE DATABASE ... FROM INTEGRATION, ALTER DATABASE ... INTEGRATION SET, and
// the SVV_INTEGRATION read.
func TestDatabaseIntegrationSQL(t *testing.T) {
	create := func(data databaseModel) func() (string, error) {
		return func() (string, error) { return createDatabaseStatement(data) }
	}
	alter := func(prev, plan databaseModel) func() ([]string, error) {
		return func() ([]string, error) { return alterDatabaseStatements(prev, plan) }
	}
	with := func(change func(*databaseModel)) databaseModel {
		data := integrationDatabaseModel()
		integration := *data.Integration
		data.Integration = &integration
		change(&data)
		return data
	}
	configured := configuredIntegrationDatabase()
	changed := with(func(data *databaseModel) {
		*data = configuredIntegrationDatabase()
		integration := *data.Integration
		integration.RefreshInterval = types.Int64Value(0)
		integration.HistoryMode, integration.AutoRemediation = types.BoolValue(false), types.BoolValue(false)
		integration.QueryAllStates, integration.AcceptInvalidChars, integration.TruncateColumns = types.BoolValue(false), types.BoolValue(true), types.BoolValue(false)
		data.Integration = &integration
	})
	unmanaged := with(func(data *databaseModel) { data.Owner = configured.Owner })
	unknown := with(func(data *databaseModel) {
		data.Integration.SourceDatabase, data.Integration.RefreshInterval = types.StringUnknown(), types.Int64Unknown()
		data.Integration.HistoryMode, data.Integration.AutoRemediation = types.BoolUnknown(), types.BoolUnknown()
	})
	checkSQL(t, "database_integration", []sqlCase{
		{"create", create(integrationDatabaseModel())},
		{"create_source_database", create(with(func(data *databaseModel) { data.Integration.SourceDatabase = types.StringValue("orders") }))},
		{"create_options", create(configured)},
		{"create_refresh_interval", create(with(func(data *databaseModel) { data.Integration.RefreshInterval = types.Int64Value(7200) }))},
		{"create_unknown_settings", create(unknown)},
		{"create_quoted", create(with(func(data *databaseModel) {
			data.Name = types.StringValue(`Odd"DB`)
			data.Integration.IntegrationID = types.StringValue(`it's-an-id`)
		}))},
		{"create_with_collation", create(with(func(data *databaseModel) { data.Collation = types.StringValue("CASE_SENSITIVE") }))},
		{"create_with_datashare", create(with(func(data *databaseModel) { data.DatashareARN = types.StringValue(shareARN) }))},
		{"create_without_integration_id", create(with(func(data *databaseModel) { data.Integration.IntegrationID = types.StringNull() }))},
		{"alter_unchanged", alter(configured, configured)},
		{"alter_settings", alter(configured, changed)},
		{"alter_from_unset", alter(integrationDatabaseModel(), configured)},
		{"alter_unmanaged", alter(configured, unmanaged)},
		{"alter_unknown", alter(configured, unknown)},
		{"read", func() (string, error) {
			sql, parameters, err := readDatabaseIntegrationQuery(`it's"replica`).Build()
			assert.Equal(t, map[string]string{"name": `it's"replica`}, parameters)
			return sql, err
		}},
	})
}

// TestDatabaseIntegrationSettingsCoverBlock keeps the rendered settings in step with the block: every field except
// the binding, which replaces the database, is a setting exactly once.
func TestDatabaseIntegrationSettingsCoverBlock(t *testing.T) {
	var response resource.SchemaResponse
	newDatabaseResource().Schema(context.Background(), resource.SchemaRequest{}, &response)
	var fields []string
	for name := range databaseIntegrationBlock().Attributes {
		if name != "integration_id" && name != "source_database" {
			fields = append(fields, name)
		}
	}
	var settings []string
	for _, setting := range databaseIntegrationSettings {
		settings = append(settings, setting.attribute)
	}
	slices.Sort(fields)
	slices.Sort(settings)
	assert.Equal(t, fields, settings)
	require.Contains(t, response.Schema.Blocks, "integration")
	assert.Len(t, databaseIntegrationAttributeTypes, len(databaseIntegrationBlock().Attributes))
}
