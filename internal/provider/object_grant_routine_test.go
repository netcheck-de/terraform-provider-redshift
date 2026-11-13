package provider

import (
	"context"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObjectGrantSnapshotLifecycle exercises a user's privileges common to all tables of a schema.
func TestObjectGrantSnapshotLifecycle(t *testing.T) {
	exercisePrivilege(t, newObjectGrantResource, map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_type": "ALL TABLES", "grantee_type": "USER", "grantee": "analyst"}, []string{"SELECT", "INSERT"})
}

// TestObjectGrantSnapshotsCoverParallelObjects repeats a scope-wide grant while objects created in parallel lack the
// privilege after its write, gives up after snapshotRounds, and never repeats a single-object grant.
func TestObjectGrantSnapshotsCoverParallelObjects(t *testing.T) {
	for _, test := range []struct {
		kind, name string
		appearing  int
		writes     int
		converges  bool
	}{
		{"ALL TABLES", "", 1, 2, true},
		{"ALL FUNCTIONS", "", 1, 2, true},
		{"ALL TABLES", "", snapshotRounds, snapshotRounds, false},
		{"TABLE", "orders", 1, 1, false},
	} {
		t.Run(test.kind+"/"+strconv.Itoa(test.appearing), func(t *testing.T) {
			privilege := "SELECT"
			if test.kind == "ALL FUNCTIONS" {
				privilege = "EXECUTE"
			}

			c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user", scope: test.kind}
			grants := 0
			r := newObjectGrantResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				rows, err := c.Query(ctx, target, sql, parameters)
				if strings.HasPrefix(sql, "GRANT ") {
					if grants++; grants <= test.appearing {
						// A new object without the privilege removes it from the privileges every object holds.
						delete(c.values, privilege)
					}
				}

				return rows, err
			}))

			fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_type": test.kind, "grantee_type": "USER", "grantee": "analyst"}
			if test.name != "" {
				fields["object_name"] = test.name
			}

			err := r.reconcile(context.Background(), privilegeObject(t, r, fields, privilege))
			if test.converges {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "did not converge")
			}

			assert.Equal(t, test.writes, grants)
		})
	}
}

// TestObjectGrantSharedSnapshotCoversParallelMembers grants a member that appears in the shared database after the
// first write in another round.
func TestObjectGrantSharedSnapshotCoversParallelMembers(t *testing.T) {
	c := &sharedSnapshotCatalog{members: []string{"first"}, privileges: map[string]map[string]bool{"first": {}}, options: map[string]map[string]bool{"first": {}}, scope: "TABLE"}
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.HasPrefix(sql, "GRANT ") && len(c.members) == 1 {
			c.members = append(c.members, "late")
			c.privileges["late"], c.options["late"] = map[string]bool{}, map[string]bool{}
		}

		return rows, err
	}))
	data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL TABLES", "grantee": "reader", "grantee_type": "USER"}, "SELECT")
	require.NoError(t, r.reconcile(context.Background(), data))

	var grants []string

	for _, write := range c.writes {
		if strings.HasPrefix(write, "GRANT ") {
			grants = append(grants, write)
		}
	}

	assert.Len(t, grants, 2)
	assert.True(t, c.privileges["late"]["SELECT"])
}

// TestObjectGrantSnapshotDeleteAfterParallelDrop treats a schema emptied in parallel during destroy as revoked.
func TestObjectGrantSnapshotDeleteAfterParallelDrop(t *testing.T) {
	c := &privilegeCatalog{values: map[string]bool{"SELECT": true}, grantee: "analyst", kind: "user", scope: "ALL TABLES"}
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.HasPrefix(sql, "REVOKE ") {
			c.missing = true
		}

		return rows, err
	}))
	data := privilegeObject(t, r, map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_type": "ALL TABLES", "grantee_type": "USER", "grantee": "analyst"}, "SELECT")
	diagnostics := invoke(t, r, "delete", data)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Contains(t, c.writes, `REVOKE SELECT ON ALL TABLES IN SCHEMA "serving" FROM "analyst"`)
}

// TestObjectGrantSnapshotRevokesPartialGrants revokes privileges and grant options that only some objects of the
// schema hold, which the snapshot read cannot report, so destroy and downgrades leave no grant behind.
func TestObjectGrantSnapshotRevokesPartialGrants(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_type": "ALL TABLES", "grantee_type": "USER", "grantee": "analyst"}
	r := newObjectGrantResource().(*privilegeResource)
	// A table created after the grant lacks SELECT, so the read reports none although the others still hold it.
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user", scope: "ALL TABLES"}
	r.resourceClient = testResourceClient(c)
	diagnostics := invoke(t, r, "delete", privilegeObject(t, r, fields, "SELECT"))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Contains(t, c.writes, `REVOKE SELECT ON ALL TABLES IN SCHEMA "serving" FROM "analyst"`)

	// Keeping SELECT without its option also revokes an option some tables still hold.
	c = &privilegeCatalog{values: map[string]bool{"SELECT": true}, grantee: "analyst", kind: "user", scope: "ALL TABLES"}
	r.resourceClient = testResourceClient(c)
	require.NoError(t, r.reconcile(context.Background(), privilegeObject(t, r, fields, "SELECT")))
	assert.Contains(t, c.writes, `REVOKE GRANT OPTION FOR SELECT ON ALL TABLES IN SCHEMA "serving" FROM "analyst"`)
	assert.NotContains(t, c.writes, `REVOKE SELECT ON ALL TABLES IN SCHEMA "serving" FROM "analyst"`)
}

// TestObjectGrantRoutineLifecycle grants EXECUTE on one function overload with its grant option, revokes it, and
// imports the tuple with its configured argument spelling.
func TestObjectGrantRoutineLifecycle(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "f_score", "object_type": "FUNCTION", "arguments": "int, varchar(10)", "grantee_type": "USER", "grantee": "analyst"}
	r := newObjectGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user"}
	r.resourceClient = testResourceClient(c)
	executeOnly := []string{"EXECUTE"}
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields, executeOnly...), executeOnly)))
	assert.Equal(t, []string{`GRANT EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(INTEGER, CHARACTER VARYING) TO "analyst" WITH GRANT OPTION`}, c.writes)
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields), nil)))
	require.Len(t, c.writes, 2)
	assert.Equal(t, `REVOKE EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(INTEGER, CHARACTER VARYING) FROM "analyst"`, c.writes[1])
	resp := resource.ImportStateResponse{State: testState(t, r, privilegeObject(t, r, fields))}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: identity(fields).ValueString()}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)

	var arguments types.String
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("arguments"), &arguments).HasError())
	assert.Equal(t, "int, varchar(10)", arguments.ValueString())
}

// TestObjectGrantRoutineValidation reports routine and snapshot tuple errors at plan time and before state.
func TestObjectGrantRoutineValidation(t *testing.T) {
	for name, fields := range map[string]map[string]string{
		"arguments on table":      {"object_type": "TABLE", "object_name": "orders", "arguments": "integer"},
		"unknown argument type":   {"object_type": "FUNCTION", "object_name": "f_score", "arguments": "money"},
		"snapshot with name":      {"object_type": "ALL TABLES", "object_name": "orders"},
		"routine without name":    {"object_type": "PROCEDURE"},
		"snapshot privilege":      {"object_type": "ALL TABLES", "privilege": "TRUNCATE"},
		"function privilege":      {"object_type": "FUNCTION", "object_name": "f_score", "privilege": "SELECT"},
		"group with grant option": {"object_type": "ALL FUNCTIONS", "grantee_type": "GROUP", "privilege": "EXECUTE", "option": "EXECUTE"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			base := map[string]string{"database_name": "warehouse", "schema_name": "serving", "grantee_type": "USER", "grantee": "analyst"}
			maps.Copy(base, fields)

			var privileges, options []string
			if fields["privilege"] != "" {
				privileges = []string{fields["privilege"]}
			}

			if fields["option"] != "" {
				options = []string{fields["option"]}
			}

			data := withGrantOptions(privilegeObject(t, r, base, privileges...), options)
			require.Error(t, r.validate(data))
			state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.True(t, diagnostics.HasError())
			assert.True(t, state.Raw.IsNull())
		})
	}
}

// TestObjectGrantRoutineSnapshotKind treats a routine snapshot over a schema that holds only the other routine kind
// as a missing grant: Read removes it and Create fails before granting, instead of granting nothing and never
// converging.
func TestObjectGrantRoutineSnapshotKind(t *testing.T) {
	for _, test := range []struct {
		kind       string
		procedures bool
	}{{"ALL FUNCTIONS", true}, {"ALL PROCEDURES", false}, {"ALL FUNCTIONS", false}, {"ALL PROCEDURES", true}} {
		matches := test.procedures == (test.kind == "ALL PROCEDURES")
		t.Run(test.kind+map[bool]string{true: " over matching routines", false: " over the other kind"}[matches], func(t *testing.T) {
			var writes []string

			r := newObjectGrantResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				switch {
				case !strings.HasPrefix(sql, "SELECT"):
					writes = append(writes, sql)
					return nil, nil
				case strings.HasPrefix(sql, "SELECT DISTINCT schema_name FROM svv_redshift_functions"):
					// The schema holds routines of one kind only, so only that kind's check finds it.
					if strings.Contains(sql, "NOT ILIKE '%PROCEDURE%'") == test.procedures {
						return nil, nil
					}

					return []sqlclient.Row{{"schema_name": "serving"}}, nil
				case strings.HasPrefix(sql, "SELECT privilege_type"):
					return nil, nil
				}

				return []sqlclient.Row{{"name": "exists"}}, nil
			}))
			fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_type": test.kind, "grantee_type": "ROLE", "grantee": "readers"}
			attributes := privilegeObject(t, r, fields).Attributes()
			attributes["id"] = identity(fields)
			state := testState(t, r, types.ObjectValueMust(privilegeObject(t, r, fields).AttributeTypes(context.Background()), attributes))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			assert.Equal(t, !matches, resp.State.Raw.IsNull(), "Read removes the snapshot only when the schema holds none of its kind")

			if matches {
				return
			}

			_, diagnostics := applyOperation(t, r, "create", nil, privilegeObject(t, r, fields, "EXECUTE"), nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics[0].Detail(), "does not exist")
			assert.Empty(t, writes, "a snapshot over no routines of its kind grants nothing")
		})
	}
}
