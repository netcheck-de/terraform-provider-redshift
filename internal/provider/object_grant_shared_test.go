package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectGrantSharedSQL(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	var cases []sqlCase
	for _, kind := range []string{"DATABASE", "SCHEMA", "TABLE"} {
		fields := map[string]string{"database_name": `shared"db`, "database_type": "shared", "object_type": kind, "grantee": "reader", "grantee_type": "USER"}
		if kind != "DATABASE" {
			fields["schema_name"] = "serving"
		}
		if kind == "TABLE" {
			fields["object_name"] = "events"
		}
		cases = append(cases, sqlCase{name: map[string]string{"DATABASE": "database", "SCHEMA": "schema", "TABLE": "table"}[kind], render: func() ([]string, error) {
			target, err := r.prepare(privilegeObject(t, r, fields))
			if err != nil {
				return nil, err
			}
			var statements []string
			for _, check := range target.checks {
				if check.sql != "" {
					statements = append(statements, check.sql)
				}
			}
			statements = append(statements, target.query.sql)
			var privilege sqlclient.Keyword = "USAGE"
			if kind == "TABLE" {
				privilege = "SELECT"
			}
			return append(statements, target.grant.statement(true, privilege), target.grant.statement(false, privilege)), nil
		}})
	}
	checkSQL(t, "object_grant_shared", cases)
}

func TestObjectGrantSharedLifecycle(t *testing.T) {
	fields := map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "grantee": "reader", "grantee_type": "USER"}
	r := newObjectGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "reader", kind: "user", scope: "TABLE"}
	r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		assert.Equal(t, "admin", connection.Database, "never connect to a shared database")
		return c.Query(ctx, connection, sql, parameters)
	}))
	data := privilegeObject(t, r, fields, "SELECT")
	for _, operation := range []string{"create", "read", "update", "delete"} {
		diagnostics := invoke(t, r, operation, data, false)
		require.False(t, diagnostics.HasError(), "%s: %v", operation, diagnostics)
	}
	assert.Equal(t, []string{`GRANT SELECT ON TABLE "shared_db"."serving"."events" TO "reader"`, `REVOKE SELECT ON TABLE "shared_db"."serving"."events" FROM "reader"`}, c.writes)
	target, err := r.target(data)
	require.NoError(t, err)
	assert.False(t, target.filter(sqlclient.Row{"identity_name": "reader", "identity_type": "user", "privilege_scope": "TABLES"}), "scoped permissions are independent")
	lookupValue(&data, "object_type", types.StringValue("MODEL"))
	require.ErrorContains(t, r.validate(data), "shared object grants")
	lookupValue(&data, "object_type", types.StringValue("TABLE"))
	lookupValue(&data, "database_type", types.StringNull())
	local, err := r.target(data)
	require.NoError(t, err)
	assert.Equal(t, "shared_db", local.database, "existing local contract is unchanged")
	assert.Contains(t, r.identity("admin", fields).ValueString(), `"database_type":"shared"`)
}

func TestObjectGrantSharedReadErrorsKeepState(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	fields := map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "grantee": "reader", "grantee_type": "USER"}
	data := privilegeObject(t, r, fields, "SELECT")
	target, err := r.target(data)
	require.NoError(t, err)
	var statements []string
	for _, check := range target.checks {
		if check.sql != "" {
			statements = append(statements, check.sql)
		}
	}
	statements = append(statements, target.query.sql)
	for _, failedSQL := range statements {
		t.Run(failedSQL, func(t *testing.T) {
			catalog := &privilegeCatalog{values: map[string]bool{"SELECT": true}, grantee: "reader", kind: "user", scope: "TABLE"}
			r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if sql == failedSQL {
					return nil, errors.New("permission denied while reading shared metadata")
				}
				return catalog.Query(ctx, connection, sql, parameters)
			}))
			state := testState(t, r, data)
			response := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
			require.True(t, response.Diagnostics.HasError())
			assert.False(t, response.State.Raw.IsNull(), "metadata access failure is not evidence of deletion")
			assert.Equal(t, state.Raw, response.State.Raw)
			assert.Empty(t, catalog.writes)
		})
	}
}
