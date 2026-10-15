package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerValidateConfigCase("database", validateConfigCase{
	new:     newDatabaseResource,
	valid:   databaseLocalOptionsModel("etl", 25, "CASE_INSENSITIVE", "SERIALIZABLE"),
	invalid: databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true), IsolationLevel: types.StringValue("SNAPSHOT")},
	unknown: databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringUnknown(), WithPermissions: types.BoolValue(true), Owner: types.StringValue("etl")},
})

// databaseLocalOptionsModel returns the local "warehouse" database the fake catalog knows, with every option set.
func databaseLocalOptionsModel(owner string, limit int64, collation, isolation string) databaseModel {
	return databaseModel{
		Name: types.StringValue("warehouse"), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true),
		Owner: types.StringValue(owner), ConnectionLimit: types.Int64Value(limit), Collation: types.StringValue(collation), IsolationLevel: types.StringValue(isolation),
	}
}

// TestDatabaseAlterCoverage checks that every in-place database option has an ALTER DATABASE step.
func TestDatabaseAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newDatabaseResource(), databaseAlterSteps)
}

// TestDatabaseOptionTranscripts records local database creation with options, refresh, in-place changes, and import.
func TestDatabaseOptionTranscripts(t *testing.T) {
	defaults := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
	configured := databaseLocalOptionsModel(`Etl"Owner`, 25, "CASE_INSENSITIVE", "SERIALIZABLE")
	changed := configured
	changed.Owner, changed.ConnectionLimit, changed.IsolationLevel = types.StringValue("admin"), types.Int64Value(-1), types.StringValue("SNAPSHOT")
	// Collation replaces the database, so an in-place update keeps it.
	altered := configured
	altered.Collation = types.StringValue("CASE_SENSITIVE")
	unconfigured := databaseLocalOptionsModel("", 0, "", "")
	unconfigured.Owner, unconfigured.ConnectionLimit = types.StringUnknown(), types.Int64Unknown()
	unconfigured.Collation, unconfigured.IsolationLevel = types.StringUnknown(), types.StringUnknown()
	existing := catalogWith(func(c *catalog) { c.localDB = true })
	created := catalogWith(func(c *catalog) {
		c.localDB = true
		f := c.family("databases").(*databasesFamily)
		f.owner, f.connectionLimit, f.collation, f.isolation = `Etl"Owner`, "25", "case_insensitive", "Serializable"
	})
	runTranscripts(t, "database_options", newDatabaseResource, []transcriptCase{
		{name: "create_options", operation: "create", catalog: catalogWith(), planned: configured},
		{name: "create_defaults", operation: "create", catalog: catalogWith(), planned: unconfigured},
		{name: "read_options", operation: "read", catalog: created, prior: configured},
		{name: "update_options", operation: "update", catalog: existing, prior: defaults, planned: altered},
		{name: "update_back", operation: "update", catalog: created, prior: configured, planned: changed},
		{name: "update_unchanged", operation: "update", catalog: created, prior: configured, planned: configured},
		{name: "import_options", operation: "import", catalog: catalogWith(), planned: configured},
	})
}

// TestDatabaseOptionsDetectDrift refreshes options changed outside Terraform and lets Update move them back.
func TestDatabaseOptionsDetectDrift(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	r := &databaseResource{testResourceClient(c)}
	data := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
	data.ID = r.identity("admin", map[string]string{"name": "warehouse"})
	fakeState[*databasesFamily](c, "databases").isolation = "Serializable"
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "SERIALIZABLE", data.IsolationLevel.ValueString())
	desired := data
	desired.IsolationLevel = types.StringValue("SNAPSHOT")
	state, diagnostics := applyOperation(t, r, "update", data, desired, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed databaseResourceModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, "SNAPSHOT", observed.IsolationLevel.ValueString())
	assert.Equal(t, []string{`ALTER DATABASE "warehouse" ISOLATION LEVEL SNAPSHOT`}, c.writes)
}

// TestDatabaseOptionsMustConverge reports an acknowledged ALTER or CREATE whose option the catalog does not hold.
func TestDatabaseOptionsMustConverge(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			c := fullCatalog()
			c.localDB = operation == "update"
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "ALTER DATABASE") {
					return nil, nil
				}
				if strings.HasPrefix(sql, "CREATE DATABASE") {
					sql = `CREATE DATABASE "warehouse"`
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &databaseResource{testResourceClient(client)}
			prior := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
			planned := databaseLocalOptionsModel("etl", 5, "CASE_SENSITIVE", "SNAPSHOT")
			state, diagnostics := applyOperation(t, r, operation, prior, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "owner is")
			if operation == "create" {
				assert.True(t, state.Raw.IsFullyKnown(), "a created database keeps fully known state")
			}
		})
	}
}

// TestDatabaseOptionsRejected covers options a shared database refuses and option values the renderer refuses.
func TestDatabaseOptionsRejected(t *testing.T) {
	c := fullCatalog()
	r := &databaseResource{testResourceClient(c)}
	shared := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true), ConnectionLimit: types.Int64Value(3)}
	for _, operation := range []string{"create", "update"} {
		_, diagnostics := applyOperation(t, r, operation, shared, shared, nil)
		require.True(t, diagnostics.HasError(), operation)
		assert.Contains(t, diagnostics.Errors()[0].Detail(), "shared databases do not accept connection_limit")
	}
	assert.Empty(t, c.writes)
	assert.Equal(t, []string{"owner", "connection_limit", "collation", "isolation_level"}, databaseLocalOptions(databaseLocalOptionsModel("etl", 1, "CASE_SENSITIVE", "SNAPSHOT")))
}

// TestDatabaseLocalMetadataFailures rejects incomplete or unrecognized local option readbacks.
func TestDatabaseLocalMetadataFailures(t *testing.T) {
	show := sqlclient.Row{"database_name": "warehouse", "database_type": "local", "database_isolation_level": "Serializable"}
	for name, test := range map[string]struct {
		show    sqlclient.Row
		options []sqlclient.Row
	}{
		"isolation":      {show: sqlclient.Row{"database_name": "warehouse", "database_type": "local", "database_isolation_level": "UNKNOWN"}},
		"missing owner":  {show: show, options: []sqlclient.Row{{"connection_limit": "5"}}},
		"no options row": {show: show},
		"limit":          {show: show, options: []sqlclient.Row{{"owner": "admin", "connection_limit": "many"}}},
	} {
		t.Run(name, func(t *testing.T) {
			r := testResourceClient(queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SHOW DATABASES"):
					return []sqlclient.Row{test.show}, nil
				case strings.HasPrefix(sql, "SELECT u.usename"):
					return test.options, nil
				default:
					assert.Fail(t, "unexpected query", "%s on %s", sql, target.Database)
					return nil, nil
				}
			}))
			_, _, err := r.databaseMetadata(context.Background(), "warehouse")
			require.Error(t, err)
		})
	}
}

// TestDatabaseCatalogValues pins the catalog spellings of isolation levels and connection limits.
func TestDatabaseCatalogValues(t *testing.T) {
	for catalog, expected := range map[string]string{"Snapshot Isolation": "SNAPSHOT", "Serializable": "SERIALIZABLE", "SERIALIZABLE": "SERIALIZABLE"} {
		level, err := databaseIsolationLevel(catalog)
		require.NoError(t, err, catalog)
		assert.Equal(t, expected, level, catalog)
	}
	_, err := databaseIsolationLevel("")
	require.Error(t, err)
	for catalog, expected := range map[string]int64{"UNLIMITED": -1, "unlimited": -1, "-1": -1, "25": 25, "0": 0} {
		limit, err := databaseConnectionLimitValue(catalog)
		require.NoError(t, err, catalog)
		assert.Equal(t, expected, limit, catalog)
	}
	for _, catalog := range []string{"", "-2", "many"} {
		_, err := databaseConnectionLimitValue(catalog)
		require.Error(t, err, catalog)
	}
}

// TestDatabaseUpdateVanished reports a database dropped between plan and apply instead of recreating it silently.
func TestDatabaseUpdateVanished(t *testing.T) {
	r := &databaseResource{testResourceClient(fullCatalog())}
	model := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
	_, diagnostics := applyOperation(t, r, "update", model, model, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "disappeared")
	var response resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &response)
	assert.Contains(t, response.Schema.Attributes["collation"].GetMarkdownDescription(), "Changing it replaces the database.")
}

// TestDatabaseCollationReadFailures turns a refused or unrecognized DB_COLLATION() read into a warning that keeps
// the current collation, so a database whose connection limit refuses the provider still refreshes.
func TestDatabaseCollationReadFailures(t *testing.T) {
	for name, test := range map[string]struct {
		rows    []sqlclient.Row
		failure error
	}{
		"no collation": {},
		"collation":    {rows: []sqlclient.Row{{"collation": "binary"}}},
		"refused":      {failure: errors.New("too many connections for database")},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			c.localDB = true
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "SELECT db_collation()") {
					assert.Equal(t, "warehouse", target.Database, "DB_COLLATION reads the database it runs in")
					return test.rows, test.failure
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &databaseResource{testResourceClient(client)}
			_, err := r.databaseCollation(context.Background(), "warehouse")
			require.Error(t, err)

			imported := databaseLocalOptionsModel("admin", -1, "", "SNAPSHOT")
			imported.ID, imported.Collation = r.identity("admin", map[string]string{"name": "warehouse"}), types.StringNull()
			state, diagnostics := applyOperation(t, r, "read", imported, nil, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Len(t, diagnostics.Warnings(), 1)
			assert.Contains(t, diagnostics.Warnings()[0].Detail(), "CONNECTION LIMIT")
			var observed databaseResourceModel
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.True(t, observed.Collation.IsNull())

			planned := databaseLocalOptionsModel("admin", 0, "CASE_INSENSITIVE", "SNAPSHOT")
			c.localDB = false
			state, diagnostics = applyOperation(t, r, "create", nil, planned, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Len(t, diagnostics.Warnings(), 1)
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Equal(t, "CASE_INSENSITIVE", observed.Collation.ValueString(), "the created collation is kept unverified")
		})
	}
}

// TestDatabaseKnownCollationNeedsNoSession refreshes, updates, and deletes a database whose collation is in state
// without opening a session inside it, where a connection limit could refuse the provider and where ALTER
// DATABASE ... ISOLATION LEVEL fails while other sessions are connected.
func TestDatabaseKnownCollationNeedsNoSession(t *testing.T) {
	c := fullCatalog()
	c.localDB, c.share = true, false
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		assert.NotEqual(t, "warehouse", target.Database, "%s", sql)
		return c.Query(ctx, target, sql, parameters)
	})
	r := &databaseResource{testResourceClient(client)}
	prior := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
	prior.ID = r.identity("admin", map[string]string{"name": "warehouse"})
	planned := prior
	planned.ConnectionLimit, planned.IsolationLevel, planned.Collation = types.Int64Value(0), types.StringValue("SERIALIZABLE"), types.StringUnknown()
	for _, operation := range []string{"read", "update", "delete"} {
		state, diagnostics := applyOperation(t, r, operation, prior, planned, nil)
		require.False(t, diagnostics.HasError(), "%s: %v", operation, diagnostics)
		assert.Empty(t, diagnostics.Warnings(), operation)
		if operation == "update" {
			var observed databaseResourceModel
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Equal(t, "CASE_SENSITIVE", observed.Collation.ValueString(), "update keeps the prior collation")
		}
	}
}
