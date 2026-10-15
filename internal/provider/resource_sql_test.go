package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executedStatement records one call that reached the SQL client.
type executedStatement struct {
	// database is the selected connection database.
	database string
	// sql is the statement text.
	sql string
	// parameters are the bound values; DDL must pass nil.
	parameters map[string]string
}

// recordingQueryFunc records every call and fails the statement equal to failOn.
func recordingQueryFunc(calls *[]executedStatement, failOn string, rows []sqlclient.Row) queryFunc {
	return func(_ context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		*calls = append(*calls, executedStatement{database: target.Database, sql: sql, parameters: parameters})
		if sql == failOn {
			return nil, errors.New("permission denied")
		}
		return rows, nil
	}
}

// TestResourceClientExec runs statements in order without parameters and stops at the first failure.
func TestResourceClientExec(t *testing.T) {
	var calls []executedStatement
	r := testResourceClient(recordingQueryFunc(&calls, "", nil))
	require.NoError(t, r.exec(context.Background(), "analytics", `ALTER USER "u" CREATEDB`, `ALTER USER "u" CONNECTION LIMIT 5`))
	assert.Equal(t, []executedStatement{
		{database: "analytics", sql: `ALTER USER "u" CREATEDB`},
		{database: "analytics", sql: `ALTER USER "u" CONNECTION LIMIT 5`},
	}, calls)
	for _, call := range calls {
		assert.Nil(t, call.parameters, "DDL is sent without a parameter map")
	}

	calls = nil
	require.NoError(t, r.exec(context.Background(), "analytics"))
	assert.Empty(t, calls, "no statements means no round trip")

	calls = nil
	r = testResourceClient(recordingQueryFunc(&calls, "SECOND", nil))
	require.ErrorContains(t, r.exec(context.Background(), "admin", "FIRST", "SECOND", "THIRD"), "permission denied")
	assert.Equal(t, []executedStatement{{database: "admin", sql: "FIRST"}, {database: "admin", sql: "SECOND"}}, calls, "later statements assume earlier ones applied")

	calls = nil
	r.warehouse.value = types.StringUnknown()
	require.ErrorContains(t, r.exec(context.Background(), "admin", "FIRST"), "must be known")
	assert.Empty(t, calls)
}

// TestResourceClientSelectRows builds the query before sending it and never sends an inconsistent one.
func TestResourceClientSelectRows(t *testing.T) {
	var calls []executedStatement
	rows := []sqlclient.Row{{"schema_name": "sales"}}
	r := testResourceClient(recordingQueryFunc(&calls, "", rows))
	query := sqlclient.Select("schema_name").From("svv_all_schemas").OptEq("database_name", "database", "analytics").OptEq("schema_name", "schema", "sales")
	actual, err := r.selectRows(context.Background(), "analytics", query)
	require.NoError(t, err)
	assert.Equal(t, rows, actual)
	assert.Equal(t, []executedStatement{{
		database:   "analytics",
		sql:        "SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema",
		parameters: map[string]string{"database": "analytics", "schema": "sales"},
	}}, calls)

	calls = nil
	_, err = r.selectRows(context.Background(), "analytics", sqlclient.Select("current_user"))
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].parameters, "queries without placeholders send no parameter map")

	calls = nil
	_, err = r.selectRows(context.Background(), "analytics", sqlclient.Select("a").From("t").Where("a = :a"))
	require.ErrorContains(t, err, "placeholder :a has no binding")
	assert.Empty(t, calls)

	r = testResourceClient(recordingQueryFunc(&calls, "SELECT current_user", nil))
	_, err = r.selectRows(context.Background(), "analytics", sqlclient.Select("current_user"))
	require.ErrorContains(t, err, "permission denied")
}

// TestResourceClientLocalDatabaseExists skips the round trip for the administration database and otherwise asks
// the catalog in it, because a dropped target database cannot be connected to.
func TestResourceClientLocalDatabaseExists(t *testing.T) {
	var calls []executedStatement
	r := testResourceClient(recordingQueryFunc(&calls, "", nil))
	exists, err := r.localDatabaseExists(context.Background(), "admin")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Empty(t, calls)

	exists, err = r.localDatabaseExists(context.Background(), "analytics")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, []executedStatement{{
		database:   "admin",
		sql:        "SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local'",
		parameters: map[string]string{"database": "analytics"},
	}}, calls)

	calls = nil
	r = testResourceClient(recordingQueryFunc(&calls, "", []sqlclient.Row{{"database_name": "analytics"}}))
	exists, err = r.localDatabaseExists(context.Background(), "analytics")
	require.NoError(t, err)
	assert.True(t, exists)

	_, err = r.localDatabaseExists(context.Background(), "")
	require.ErrorContains(t, err, "is empty", "the Data API rejects empty parameters, so the query is refused locally")
}
