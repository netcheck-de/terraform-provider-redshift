package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerValidateConfigCase("database_integration", validateConfigCase{
	new:   newDatabaseResource,
	valid: configuredIntegrationDatabase(),
	invalid: func() databaseModel {
		data := integrationDatabaseModel()
		data.Collation = types.StringValue("CASE_SENSITIVE")
		return data
	}(),
	// An unknown collation may still conflict, which Create checks once it is known.
	unknown: func() databaseModel {
		data := integrationDatabaseModel()
		data.Collation, data.Integration.IntegrationID = types.StringUnknown(), types.StringUnknown()
		return data
	}(),
})

// integrationCatalog returns a fake factory whose destination database exists or not, after the given changes.
func integrationCatalog(exists bool, changes ...func(*databaseIntegrationFamily)) func() sqlclient.Client {
	return catalogWith(func(c *catalog) {
		f := c.family("database_integration").(*databaseIntegrationFamily)
		f.exists = exists
		for _, change := range changes {
			change(f)
		}
	})
}

// integrationState returns the configured destination as a refresh after creation stores it.
func integrationState() databaseModel {
	data := configuredIntegrationDatabase()
	data.ID = types.StringValue(`{"workgroup_name":"warehouse","database":"admin","name":"replica","integration_id":"` + fakeIntegrationID + `"}`)
	data.DatabaseType = types.StringValue("INTEGRATION")
	return data
}

// configuredIntegrationCatalog is the catalog state after creating configuredIntegrationDatabase.
func configuredIntegrationCatalog(f *databaseIntegrationFamily) {
	f.sourceDatabase, f.refreshInterval, f.historyMode, f.autoRemediation = `it's"orders`, "600", "t", "t"
	f.owner, f.connectionLimit, f.isolation = `Etl"Owner`, "25", "Serializable"
}

// TestDatabaseIntegrationTranscripts records creating, refreshing, updating, deleting, and importing a zero-ETL
// destination database.
func TestDatabaseIntegrationTranscripts(t *testing.T) {
	minimal := integrationDatabaseModel()
	minimal.Owner, minimal.ConnectionLimit, minimal.IsolationLevel, minimal.Collation = types.StringUnknown(), types.Int64Unknown(), types.StringUnknown(), types.StringUnknown()
	integration := *minimal.Integration
	integration.SourceDatabase, integration.RefreshInterval = types.StringUnknown(), types.Int64Unknown()
	integration.HistoryMode, integration.AutoRemediation = types.BoolUnknown(), types.BoolUnknown()
	minimal.Integration = &integration
	state := integrationState()
	changed := integrationState()
	settings := *changed.Integration
	settings.RefreshInterval, settings.HistoryMode, settings.QueryAllStates = types.Int64Value(0), types.BoolValue(false), types.BoolValue(false)
	changed.Integration = &settings
	changed.ConnectionLimit = types.Int64Value(-1)
	runTranscripts(t, "database_integration_flows", newDatabaseResource, []transcriptCase{
		{name: "create", operation: "create", catalog: integrationCatalog(false), planned: configuredIntegrationDatabase()},
		{name: "create_minimal", operation: "create", catalog: integrationCatalog(false), planned: minimal},
		{name: "read", operation: "read", catalog: integrationCatalog(true, configuredIntegrationCatalog), prior: state},
		{name: "update_settings", operation: "update", catalog: integrationCatalog(true, configuredIntegrationCatalog), prior: state, planned: changed},
		{name: "update_unchanged", operation: "update", catalog: integrationCatalog(true, configuredIntegrationCatalog), prior: state, planned: state},
		{name: "delete", operation: "delete", catalog: integrationCatalog(true, configuredIntegrationCatalog), prior: state},
		{name: "import", operation: "import", catalog: integrationCatalog(false), planned: configuredIntegrationDatabase()},
	})
}

// TestDatabaseIntegrationImport restores the integration block from the import identity and leaves the settings no
// catalog view reports null.
func TestDatabaseIntegrationImport(t *testing.T) {
	c := fullCatalog()
	configuredIntegrationCatalog(fakeState[*databaseIntegrationFamily](c, "database_integration"))
	r := newDatabaseResource()
	configureTestResource(t, r, c)
	ctx := context.Background()
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: integrationState().ID.ValueString()}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	resp := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var data databaseResourceModel
	require.False(t, resp.State.Get(ctx, &data).HasError())
	require.NotNil(t, data.Integration)
	assert.Equal(t, fakeIntegrationID, data.Integration.IntegrationID.ValueString())
	assert.Equal(t, `it's"orders`, data.Integration.SourceDatabase.ValueString())
	assert.Equal(t, int64(600), data.Integration.RefreshInterval.ValueInt64())
	assert.True(t, data.Integration.HistoryMode.ValueBool())
	assert.True(t, data.Integration.QueryAllStates.IsNull(), "not read back")
	assert.Equal(t, "INTEGRATION", data.DatabaseType.ValueString())
	assert.Equal(t, `Etl"Owner`, data.Owner.ValueString())
	assert.True(t, data.Collation.IsNull(), "a destination reports no collation")
	assert.Empty(t, c.writes)
}

// TestDatabaseIntegrationDrift refreshes readable settings changed outside Terraform, keeps the unreadable ones, and
// lets Update move them back.
func TestDatabaseIntegrationDrift(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	configuredIntegrationCatalog(f)
	f.refreshInterval, f.autoRemediation, f.settings["QUERY_ALL_STATES"] = "3600", "f", "FALSE"
	r := &databaseResource{testResourceClient(c)}
	data := integrationState()
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(3600), data.Integration.RefreshInterval.ValueInt64())
	assert.False(t, data.Integration.AutoRemediation.ValueBool())
	assert.True(t, data.Integration.QueryAllStates.ValueBool(), "drift of unreadable settings is not detected")
	state, diagnostics := applyOperation(t, r, "update", data, integrationState(), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed databaseResourceModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, int64(600), observed.Integration.RefreshInterval.ValueInt64())
	assert.Equal(t, []string{
		`ALTER DATABASE "replica" INTEGRATION SET REFRESH_INTERVAL 600`,
		`ALTER DATABASE "replica" INTEGRATION SET AUTO_REMEDIATION = TRUE`,
	}, c.writes)
}

// TestDatabaseIntegrationMustConverge reports an acknowledged ALTER whose setting the catalog does not hold.
func TestDatabaseIntegrationMustConverge(t *testing.T) {
	c := fullCatalog()
	configuredIntegrationCatalog(fakeState[*databaseIntegrationFamily](c, "database_integration"))
	r := &databaseResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if sql == `ALTER DATABASE "replica" INTEGRATION SET REFRESH_INTERVAL 0` {
			return nil, nil
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	planned := integrationState()
	integration := *planned.Integration
	integration.RefreshInterval = types.Int64Value(0)
	planned.Integration = &integration
	state := testState(t, r, integrationState())
	plan := withTimeouts(t, testState(t, r, planned), map[string]string{"update": "10ms"})
	response := resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, &response)
	require.True(t, response.Diagnostics.HasError())
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "integration.refresh_interval is 600")
}

// TestDatabaseIntegrationDelayedReadback covers acknowledged ALTERs whose catalog settings are initially stale.
func TestDatabaseIntegrationDelayedReadback(t *testing.T) {
	c := fullCatalog()
	configuredIntegrationCatalog(fakeState[*databaseIntegrationFamily](c, "database_integration"))
	readsAfterAlter := 0
	altered := false
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, `ALTER DATABASE "replica" INTEGRATION SET REFRESH_INTERVAL`) {
			altered = true
		}
		rows, err := c.Query(ctx, target, sql, parameters)
		if altered && strings.HasPrefix(sql, "SELECT TRIM(integration_id)") {
			readsAfterAlter++
			if readsAfterAlter == 1 && len(rows) == 1 {
				rows[0]["refresh_interval"] = "600"
			}
		}
		return rows, err
	})
	r := &databaseResource{testResourceClient(client)}
	planned := integrationState()
	integration := *planned.Integration
	integration.RefreshInterval = types.Int64Value(1800)
	planned.Integration = &integration
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state := testState(t, r, integrationState())
	response := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan(testState(t, r, planned))}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	assert.Equal(t, 2, readsAfterAlter)
	var observed databaseResourceModel
	require.False(t, response.State.Get(ctx, &observed).HasError())
	assert.Equal(t, int64(1800), observed.Integration.RefreshInterval.ValueInt64())
}

// TestDatabaseIntegrationReadFailures rejects a destination the provider cannot verify instead of adopting it.
func TestDatabaseIntegrationReadFailures(t *testing.T) {
	for name, test := range map[string]struct {
		change func(*databaseIntegrationFamily)
		error  string
	}{
		"other integration": {change: func(f *databaseIntegrationFamily) { f.integrationID = "other" }, error: `receives integration "other"`},
		"shared":            {change: func(f *databaseIntegrationFamily) { f.databaseType = "shared" }, error: "shared database"},
		"interval":          {change: func(f *databaseIntegrationFamily) { f.refreshInterval = "soon" }, error: "refresh_interval"},
		"history mode":      {change: func(f *databaseIntegrationFamily) { f.historyMode = "maybe" }, error: "is_history_mode"},
		"no identifier":     {change: func(f *databaseIntegrationFamily) { f.integrationID = " " }, error: "no integration_id"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			test.change(fakeState[*databaseIntegrationFamily](c, "database_integration"))
			r := &databaseResource{testResourceClient(c)}
			data := integrationState()
			_, err := r.read(context.Background(), &data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.error)
		})
	}
	// A destination read as a local database is refused, so it is never adopted without its integration.
	r := &databaseResource{testResourceClient(fullCatalog())}
	local := integrationState()
	local.Integration = nil
	_, err := r.read(context.Background(), &local)
	require.ErrorContains(t, err, "not a local database")
}

// TestDatabaseIntegrationHidden keeps a destination whose integration SVV_INTEGRATION no longer shows: refresh warns
// and drops the block from state, so the plan replaces the database, destroy still drops it, and creation fails
// verification.
func TestDatabaseIntegrationHidden(t *testing.T) {
	hidden := func() *catalog {
		c := fullCatalog()
		f := fakeState[*databaseIntegrationFamily](c, "database_integration")
		configuredIntegrationCatalog(f)
		f.hidden = true
		return c
	}
	r := newDatabaseResource()
	configureTestResource(t, r, hidden())
	state, diagnostics := applyOperation(t, r, "read", integrationState(), nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, diagnostics.Warnings(), 1)
	assert.Contains(t, diagnostics.Warnings()[0].Detail(), "plans a replacement")
	var data databaseResourceModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Nil(t, data.Integration)
	assert.Equal(t, fakeIntegrationDatabase, data.Name.ValueString())

	c := hidden()
	r = newDatabaseResource()
	configureTestResource(t, r, c)
	_, diagnostics = applyOperation(t, r, "delete", integrationState(), nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.False(t, fakeState[*databaseIntegrationFamily](c, "database_integration").exists)

	c = hidden()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	f.exists = false
	r = newDatabaseResource()
	configureTestResource(t, r, c)
	plan := withTimeouts(t, testState(t, r, configuredIntegrationDatabase()), map[string]string{"create": "10ms"})
	response := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &response)
	require.True(t, response.Diagnostics.HasError())
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "only superusers")
	require.False(t, response.State.Get(context.Background(), &data).HasError())
	require.NotNil(t, data.Integration, "apply verification must keep the binding even when visibility times out")
	assert.Equal(t, fakeIntegrationID, data.Integration.IntegrationID.ValueString())
	f.hidden = false
	refreshed := resource.ReadResponse{State: response.State}
	r.Read(context.Background(), resource.ReadRequest{State: response.State}, &refreshed)
	require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)
	require.False(t, refreshed.State.Get(context.Background(), &data).HasError())
	require.NotNil(t, data.Integration, "a later refresh must discover the binding after apply-time visibility recovers")
	assert.Equal(t, fakeIntegrationID, data.Integration.IntegrationID.ValueString())
}

// TestDatabaseIntegrationAbsentThenVisible separates apply-time visibility lag from ordinary deletion refresh.
func TestDatabaseIntegrationAbsentThenVisible(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			c := fullCatalog()
			f := fakeState[*databaseIntegrationFamily](c, "database_integration")
			configuredIntegrationCatalog(f)
			f.exists = operation == "update"
			written, hiddenReads := false, 0
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "CREATE DATABASE") || strings.HasPrefix(sql, "ALTER DATABASE") {
					written = true
				}
				if written && strings.HasPrefix(sql, "SELECT TRIM(integration_id)") && hiddenReads < 2 {
					hiddenReads++
					return nil, nil
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &databaseResource{testResourceClient(client)}
			planned := configuredIntegrationDatabase()
			if operation == "update" {
				planned.Integration.RefreshInterval = types.Int64Value(1800)
			}
			state, diagnostics := applyOperation(t, r, operation, integrationState(), planned, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, 2, hiddenReads)
			var observed databaseResourceModel
			require.False(t, state.Get(context.Background(), &observed).HasError())
			require.NotNil(t, observed.Integration)
			assert.Equal(t, fakeIntegrationID, observed.Integration.IntegrationID.ValueString())
			assert.Equal(t, planned.Integration.RefreshInterval, observed.Integration.RefreshInterval)
		})
	}
}

func TestDatabaseIntegrationInitiallyAbsentBeforeUpdate(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	configuredIntegrationCatalog(f)
	f.refreshInterval = "3600"
	hiddenReads := 0
	r := &databaseResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT TRIM(integration_id)") && hiddenReads < 2 {
			hiddenReads++
			return nil, nil
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	planned := integrationState()
	integration := *planned.Integration
	integration.RefreshInterval = types.Int64Value(1800)
	planned.Integration = &integration
	state := testState(t, r, integrationState())
	plan := withTimeouts(t, testState(t, r, planned), map[string]string{"update": "5s"})
	response := resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	var observed databaseResourceModel
	require.False(t, response.State.Get(context.Background(), &observed).HasError())
	require.NotNil(t, observed.Integration)
	assert.Equal(t, int64(1800), observed.Integration.RefreshInterval.ValueInt64(), "initial visibility retries must not wait for stale prior settings")
}

// TestDatabaseIntegrationDeleteActive explains the teardown order when Redshift refuses to drop a destination whose
// integration still replicates, and keeps the database in state.
func TestDatabaseIntegrationDeleteActive(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	configuredIntegrationCatalog(f)
	f.active = true
	r := newDatabaseResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "delete", integrationState(), nil, nil)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Delete zero-ETL destination database", diagnostics.Errors()[0].Summary())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "being accessed by integration client")
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "Delete the integration outside Terraform")
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "aws redshift delete-integration (DynamoDB)")
	assert.True(t, f.exists)
}

// TestDatabaseIntegrationLocalType accepts a destination that SHOW DATABASES reports as local, since its type is
// undocumented, and still reads neither its collation nor the integration settings no view reports.
func TestDatabaseIntegrationLocalType(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	f.exists, f.databaseType = false, "local"
	r := newDatabaseResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "create", nil, configuredIntegrationDatabase(), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data databaseResourceModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, databaseTypeLocal, data.DatabaseType.ValueString())
	assert.True(t, data.Collation.IsNull())
	assert.True(t, data.Integration.TruncateColumns.ValueBool(), "the configured value is kept")
	assert.Equal(t, map[string]string{"ACCEPTINVCHARS": "FALSE", "QUERY_ALL_STATES": "TRUE", "TRUNCATECOLUMNS": "TRUE"}, f.settings)
}

// TestDatabaseIntegrationFailures injects a failure at every SQL call of each operation, including after the DDL
// succeeded, and checks that every one is reported; an empty catalog fails creation and update only.
func TestDatabaseIntegrationFailures(t *testing.T) {
	state := integrationState()
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			for failAt := 0; failAt <= calls; failAt++ {
				c := fullCatalog()
				f := fakeState[*databaseIntegrationFamily](c, "database_integration")
				f.exists = operation != "create"
				configuredIntegrationCatalog(f)
				count := 0
				r := newDatabaseResource()
				configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
					count++
					if count == failAt {
						return nil, errors.New("injected API failure")
					}
					return c.Query(ctx, target, sql, parameters)
				}))
				planned := any(state)
				if operation == "create" {
					planned = configuredIntegrationDatabase()
				}
				_, diagnostics := applyOperation(t, r, operation, state, planned, nil)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					calls = count
				} else {
					require.True(t, diagnostics.HasError(), "query %d failure must be reported", failAt)
				}
			}
			r := newDatabaseResource()
			configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return nil, nil
			}))
			after, diagnostics := applyOperation(t, r, operation, state, state, nil)
			if operation == "read" {
				// applyOperation reports the removal as an error; Read itself only removes the resource.
				assert.True(t, after.Raw.IsNull(), "a vanished destination is removed from state")
				return
			}
			assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestDatabaseIntegrationRejected refuses conflicting or incomplete integration blocks before any SQL.
func TestDatabaseIntegrationRejected(t *testing.T) {
	for name, change := range map[string]func(*databaseModel){
		"datashare": func(data *databaseModel) { data.DatashareARN = types.StringValue(shareARN) },
		"collation": func(data *databaseModel) { data.Collation = types.StringValue("CASE_SENSITIVE") },
		"no id":     func(data *databaseModel) { data.Integration.IntegrationID = types.StringNull() },
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			fakeState[*databaseIntegrationFamily](c, "database_integration").exists = false
			r := &databaseResource{testResourceClient(c)}
			data := integrationDatabaseModel()
			change(&data)
			_, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "integration")
			assert.Empty(t, c.writes)
		})
	}
}

// TestDatabaseLookupIntegrationDenied keeps ordinary database lookup available to non-superusers without hiding
// unrelated catalog failures or weakening the resource's verification of a configured integration.
func TestDatabaseLookupIntegrationDenied(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT TRIM(integration_id)") {
			return nil, errors.New("ERROR: permission denied for relation svv_integration (SQLSTATE 42501)")
		}
		return c.Query(ctx, target, sql, parameters)
	})
	state, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue("warehouse")}, client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, diagnostics.Warnings(), 1)
	assert.Contains(t, diagnostics.Warnings()[0].Detail(), "requires a superuser")
	var data databaseData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Nil(t, data.Integration)
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, "CASE_SENSITIVE", data.Collation.ValueString())
	r := &databaseResource{testResourceClient(client)}
	_, _, err := r.databaseIntegration(context.Background(), "warehouse")
	require.ErrorContains(t, err, "permission denied")
}

// TestDatabaseIntegrationReplacement covers both branches of the block's conditional replacement: adding or
// removing it and changing the binding replace, also to a value known only at apply, while the settings and an
// omitted source database do not.
func TestDatabaseIntegrationReplacement(t *testing.T) {
	object := func(change func(map[string]attr.Value)) types.Object {
		values := map[string]attr.Value{
			"integration_id": types.StringValue(fakeIntegrationID), "source_database": types.StringValue("orders"),
			"refresh_interval": types.Int64Value(0), "history_mode": types.BoolValue(false), "auto_remediation": types.BoolValue(false),
			"query_all_states": types.BoolNull(), "accept_invalid_chars": types.BoolNull(), "truncate_columns": types.BoolNull(),
		}
		if change != nil {
			change(values)
		}
		return types.ObjectValueMust(databaseIntegrationAttributeTypes, values)
	}
	null, unknown := types.ObjectNull(databaseIntegrationAttributeTypes), types.ObjectUnknown(databaseIntegrationAttributeTypes)
	unknownID := object(func(v map[string]attr.Value) { v["integration_id"] = types.StringUnknown() })
	unknownSource := object(func(v map[string]attr.Value) { v["source_database"] = types.StringUnknown() })
	for name, test := range map[string]struct {
		state, plan, config types.Object
		replace             bool
	}{
		"added":                  {state: null, plan: object(nil), replace: true},
		"removed":                {state: object(nil), plan: null, replace: true},
		"absent":                 {state: null, plan: null},
		"unknown block":          {state: object(nil), plan: unknown, replace: true},
		"integration_id":         {state: object(nil), plan: object(func(v map[string]attr.Value) { v["integration_id"] = types.StringValue("other") }), replace: true},
		"integration_id unknown": {state: object(nil), plan: unknownID, replace: true},
		"source_database": {state: object(nil), plan: object(func(v map[string]attr.Value) {
			v["source_database"] = types.StringValue("sales")
		}), replace: true},
		"source_database computed": {state: object(nil), plan: unknownSource, config: object(func(v map[string]attr.Value) { v["source_database"] = types.StringNull() })},
		"source_database unknown":  {state: object(nil), plan: unknownSource, replace: true},
		"settings": {state: object(nil), plan: object(func(v map[string]attr.Value) {
			v["refresh_interval"], v["history_mode"], v["query_all_states"] = types.Int64Value(600), types.BoolValue(true), types.BoolValue(true)
		})},
	} {
		t.Run(name, func(t *testing.T) {
			config := test.config
			if config.IsNull() && !test.plan.IsNull() {
				config = test.plan
			}
			request := planmodifier.ObjectRequest{Path: path.Root("integration"), StateValue: test.state, PlanValue: test.plan, ConfigValue: config}
			var response objectplanmodifier.RequiresReplaceIfFuncResponse
			databaseIntegrationReplacement(context.Background(), request, &response)
			assert.Equal(t, test.replace, response.RequiresReplace)
		})
	}
}

// TestDatabaseIntegrationLookup reports a destination's integration and an import-compatible identity, and a null
// integration when SVV_INTEGRATION hides it.
func TestDatabaseIntegrationLookup(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		c := fullCatalog()
		f := fakeState[*databaseIntegrationFamily](c, "database_integration")
		configuredIntegrationCatalog(f)
		f.hidden = hidden
		state, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue(fakeIntegrationDatabase)}, c)
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var data databaseData
		require.False(t, state.Get(context.Background(), &data).HasError())
		assert.Equal(t, "INTEGRATION", data.DatabaseType.ValueString())
		assert.Equal(t, `Etl"Owner`, data.Owner.ValueString())
		if hidden {
			assert.Nil(t, data.Integration)
			assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","name":"replica"}`, data.ID.ValueString())
			continue
		}
		require.NotNil(t, data.Integration)
		assert.Equal(t, fakeIntegrationID, data.Integration.IntegrationID.ValueString())
		assert.Equal(t, int64(600), data.Integration.RefreshInterval.ValueInt64())
		assert.True(t, data.Integration.AutoRemediation.ValueBool())
		assert.True(t, data.Integration.TruncateColumns.IsNull())
		assert.JSONEq(t, integrationState().ID.ValueString(), data.ID.ValueString())
	}
	// A destination that SHOW DATABASES reports as local still reports its integration, and no collation.
	c := fullCatalog()
	f := fakeState[*databaseIntegrationFamily](c, "database_integration")
	configuredIntegrationCatalog(f)
	f.databaseType = "local"
	state, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue(fakeIntegrationDatabase)}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var local databaseData
	require.False(t, state.Get(context.Background(), &local).HasError())
	assert.Equal(t, databaseTypeLocal, local.DatabaseType.ValueString())
	require.NotNil(t, local.Integration)
	assert.Equal(t, fakeIntegrationID, local.Integration.IntegrationID.ValueString())
	assert.True(t, local.Collation.IsNull())

	failing := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT TRIM(integration_id)") {
			return nil, errors.New("permission denied")
		}
		return fullCatalog().Query(ctx, target, sql, parameters)
	})
	_, diagnostics = readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue(fakeIntegrationDatabase)}, failing)
	assert.True(t, diagnostics.HasError())
}
