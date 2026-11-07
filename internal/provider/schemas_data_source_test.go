package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newSchemasDataSource, resource: newSchemaResource, collection: true, filters: []string{"database"}})

// TestSchemasDataSource lists the local schemas of the provider database and of a selected database with the quotas
// the lookup reports.
func TestSchemasDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/schemas", "default_database", newSchemasDataSource, nil)
	assert.Equal(t, []string{"public", "serving"}, discoveryNames(items, "name"))
	assert.Equal(t, types.StringValue("admin"), items[0]["database"])
	assert.Equal(t, types.StringValue("rdsdb"), items[0]["owner"])
	assert.Equal(t, types.Int64Value(schemaUnlimited), items[0]["quota"], "a superuser sees no quota row as UNLIMITED")
	assert.Equal(t, types.Int64Value(schemaUnlimited), items[1]["quota"])

	items = discoveryTranscript(t, "discovery/schemas", "database", newSchemasDataSource, map[string]string{"database": "analytics"})
	assert.Equal(t, types.StringValue("analytics"), items[1]["database"])

	_, identity, diagnostics := discoveryRead(t, newSchemasDataSource, map[string]string{"database": "analytics"}, fullCatalog())
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, map[string]string{"database": "analytics"}, identity)
}

// TestSchemasDataSourceQuotaVisibility reports a quota a regular user may see and null for another owner's schema
// without a visible quota row, as the lookup does.
func TestSchemasDataSourceQuotaVisibility(t *testing.T) {
	for _, test := range []struct {
		name, owner string
		serving     attr.Value
	}{
		{"own schema", "etl", types.Int64Value(2048)},
		{"other owner", "admin", types.Int64Null()},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := catalogWith(func(c *catalog) {
				f := c.family("databases").(*databasesFamily)
				f.schemaOwner, f.schemaQuota, f.sessionUser, f.sessionRegular = test.owner, "2048", "etl", true
			})()
			items, _, diagnostics := discoveryRead(t, newSchemasDataSource, nil, client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Len(t, items, 2)
			assert.True(t, items[0]["quota"].IsNull(), "rdsdb's public schema quota is hidden from a regular user")
			assert.Equal(t, test.serving, items[1]["quota"])
		})
	}
}

// TestSchemasDataSourceQueries reads in the selected database with a fixed number of queries and reports catalog
// failures and malformed rows.
func TestSchemasDataSourceQueries(t *testing.T) {
	answer := func(schemas []sqlclient.Row, fail string) sqlclient.Client {
		return withLocalDatabases(queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if connection.Database != "analytics" {
				return nil, errors.New("unexpected database " + connection.Database)
			}

			if fail != "" && strings.Contains(sql, fail) {
				return nil, errors.New("catalog unavailable")
			}

			switch {
			case strings.Contains(sql, "pg_external_schema"):
				return schemas, nil
			case strings.Contains(sql, "svv_redshift_schema_quota"):
				return []sqlclient.Row{{"schema_name": "padded", "quota": "4096"}}, nil
			default:
				return []sqlclient.Row{{"name": "admin", "superuser": "true"}}, nil
			}
		}))
	}

	schemas := []sqlclient.Row{{"schema_name": "padded ", "owner": "etl"}, {"schema_name": "plain", "owner": "etl"}}
	recorder := &recordingClient{client: answer(schemas, "")}
	items, _, diagnostics := discoveryRead(t, newSchemasDataSource, map[string]string{"database": "analytics"}, recorder)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Len(t, recorder.take(), 4, "one query each for the database check, schemas, quotas, and the session")
	assert.Equal(t, types.StringValue("padded "), items[0]["name"], "quoted names keep their spaces")
	assert.Equal(t, types.Int64Value(4096), items[0]["quota"], "the trimmed quota row matches the padded name")
	assert.Equal(t, types.Int64Value(schemaUnlimited), items[1]["quota"])

	for _, fail := range []string{"pg_external_schema", "svv_redshift_schema_quota", "usesuper"} {
		_, _, diagnostics = discoveryRead(t, newSchemasDataSource, map[string]string{"database": "analytics"}, answer(schemas, fail))
		assert.True(t, diagnostics.HasError(), "a failing %s query is an error", fail)
	}

	_, _, diagnostics = discoveryRead(t, newSchemasDataSource, map[string]string{"database": "analytics"}, answer([]sqlclient.Row{{"schema_name": "orphan", "owner": ""}}, ""))
	assert.True(t, diagnostics.HasError(), "a schema without an owner is malformed")
}
