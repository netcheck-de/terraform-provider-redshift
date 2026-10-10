package provider

import (
	"context"
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

// TestObjectGrantRoutineLifecycle grants EXECUTE on one function overload with its grant option, revokes it, and
// imports the tuple with its configured argument spelling.
func TestObjectGrantRoutineLifecycle(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "f_score", "object_type": "FUNCTION", "arguments": "int, varchar(10)", "grantee_type": "USER", "grantee": "analyst"}
	r := newObjectGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user"}
	r.resourceClient = testResourceClient(c)
	executeOnly := []string{"EXECUTE"}
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields, executeOnly...), executeOnly)))
	assert.Equal(t, []string{`GRANT EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(integer, character varying) TO "analyst" WITH GRANT OPTION`}, c.writes)
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields), nil)))
	require.Len(t, c.writes, 2)
	assert.Equal(t, `REVOKE EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(integer, character varying) FROM "analyst"`, c.writes[1])
	resp := resource.ImportStateResponse{State: testState(t, r, privilegeObject(t, r, fields))}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: r.identity("admin", fields).ValueString()}, &resp)
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
			for key, value := range fields {
				base[key] = value
			}
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
			attributes["id"] = r.identity("admin", fields)
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
