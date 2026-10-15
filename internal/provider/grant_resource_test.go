package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerValidateConfigCase("grant", validateConfigCase{
	new:     newGrantResource,
	valid:   grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("DATABASE"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})}),
	invalid: grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})}),
	unknown: grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA"), SchemaName: types.StringUnknown(), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})}),
})

var _ = registerLifecycleCase(lifecycleCase{
	name: "grant", kind: lifecyclePermission, new: newGrantResource,
	model:  grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("example:readers"), Scope: types.StringValue("TABLES"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}),
	absent: func(c *catalog) { clear(c.privileges) },
	prepare: func(c *catalog, operation string) {
		if operation == "update" {
			// Revoking a different privilege first shows the REVOKE-before-GRANT ordering.
			c.privileges = map[string]bool{"INSERT": true}
		}
	},
})

var _ = registerReplacementPolicy("redshift_grant", map[string]replaceRule{
	"database_name":           replaceAlways,
	"schema_name":             replaceAlways,
	"role":                    replaceAlways,
	"user":                    replaceAlways,
	"datashare":               replaceAlways,
	"scope":                   replaceAlways,
	"privileges":              replaceNever,
	"grant_option_privileges": replaceNever,
})

// grantTestModel fills the sets a test model leaves zero, which the framework cannot convert, with empty sets.
func grantTestModel(data grantModel) grantModel {
	for _, set := range []*types.Set{&data.Privileges, &data.GrantOptionPrivileges} {
		if set.ElementType(context.Background()) == nil {
			*set = types.SetValueMust(types.StringType, nil)
		}
	}
	return data
}

// TestGrantObservesExactPrivileges checks scoped privilege refresh and reconciliation.
func TestGrantObservesExactPrivileges(t *testing.T) {
	c := &catalog{role: true, database: true, privileges: map[string]bool{"SELECT": true, "INSERT": true}}
	r := &grantResource{testResourceClient(c)}
	data := grantModel{
		DatabaseName: types.StringValue("analytics"),
		Role:         types.StringValue("example:readers"), Scope: types.StringValue("TABLES"),
	}
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, data.Privileges.Elements(), 2)
	delete(c.privileges, "INSERT")
	_, found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, data.Privileges.Elements(), 1)
}

// TestGrantConnectionContext checks local and shared database routing.
func TestGrantConnectionContext(t *testing.T) {
	for _, databaseType := range []string{"local", "shared"} {
		t.Run(databaseType, func(t *testing.T) {
			var connections []string
			r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, target dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				connections = append(connections, target.Database)
				switch {
				case strings.HasPrefix(sql, "SELECT database_type"):
					return []dataapi.Row{{"database_type": databaseType}}, nil
				case strings.HasPrefix(sql, "SELECT role_name"):
					return []dataapi.Row{{"role_name": "readers"}}, nil
				default:
					return []dataapi.Row{
						{"database_name": "analytics", "identity_name": "readers", "object_type": "DATABASE", "privilege_scope": "TABLES", "privilege_type": "SELECT"},
						{"database_name": "analytics", "identity_name": "other", "object_type": "DATABASE", "privilege_scope": "TABLES", "privilege_type": "INSERT"},
						{"database_name": "analytics", "identity_name": "readers", "object_type": "DATABASE", "privilege_scope": "SCHEMAS", "privilege_type": "USAGE"},
					}, nil
				}
			}))}
			data := grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("TABLES")}
			_, found, err := r.read(context.Background(), &data)
			last := "admin"
			if databaseType == "local" {
				last = "analytics"
			}
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, []string{"admin", "admin", last}, connections)
			assert.Len(t, data.Privileges.Elements(), 1)
		})
	}
}

// TestGrantPreflightsUnsupportedPrivileges ensures unexpected catalog permissions fail before mutation.
func TestGrantPreflightsUnsupportedPrivileges(t *testing.T) {
	writes := 0
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT database_type"):
			return []dataapi.Row{{"database_type": "shared"}}, nil
		case strings.HasPrefix(sql, "SELECT role_name"):
			return []dataapi.Row{{"role_name": "readers"}}, nil
		case strings.HasPrefix(sql, "SHOW"):
			return []dataapi.Row{{"database_name": "analytics", "identity_name": "readers", "object_type": "DATABASE", "privilege_scope": "TABLES", "privilege_type": "UNSUPPORTED"}}, nil
		default:
			writes++
			return nil, fmt.Errorf("unexpected mutation")
		}
	}))}
	data := grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("TABLES"), Privileges: types.SetValueMust(types.StringType, nil)}
	require.ErrorContains(t, r.reconcile(context.Background(), data), "unsupported catalog privilege")
	assert.Zero(t, writes)
}

// TestGrantRejectsUnsupportedTargets checks unknown connections and unsupported database kinds.
func TestGrantRejectsUnsupportedTargets(t *testing.T) {
	for _, name := range []string{"unknown connection", "external database"} {
		t.Run(name, func(t *testing.T) {
			r := &grantResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return []dataapi.Row{{"database_type": "external"}}, nil
			}))}
			data := grantModel{DatabaseName: types.StringValue("analytics")}
			if name == "unknown connection" {
				r.warehouse.value = types.StringUnknown()
			}
			_, _, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}

// TestSchemaGrantsUseSchemaObjectAndScope checks explicit-schema and scoped-table SQL.
func TestSchemaGrantsUseSchemaObjectAndScope(t *testing.T) {
	for scope, privilege := range map[string]string{"SCHEMA": "USAGE", "TABLES": "SELECT"} {
		t.Run(scope, func(t *testing.T) {
			granted := false
			var mutations []string
			r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, target dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SELECT database_type"):
					return []dataapi.Row{{"database_type": "local"}}, nil
				case strings.HasPrefix(sql, "SELECT role_name"):
					return []dataapi.Row{{"role_name": "example:readers"}}, nil
				case strings.HasPrefix(sql, "SELECT schema_name"):
					return []dataapi.Row{{"schema_name": "serving"}}, nil
				case strings.HasPrefix(sql, "SHOW GRANTS"):
					assert.Equal(t, "analytics", target.Database)
					if granted {
						return []dataapi.Row{{"database_name": "analytics", "schema_name": "serving", "object_type": "SCHEMA", "privilege_scope": scope, "identity_name": "example:readers", "privilege_type": privilege}}, nil
					}
					return nil, nil
				case strings.HasPrefix(sql, "GRANT "):
					mutations = append(mutations, sql)
					granted = true
					return nil, nil
				default:
					return nil, fmt.Errorf("unexpected SQL %q", sql)
				}
			}))}
			data := grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Role: types.StringValue("example:readers"), Scope: types.StringValue(scope), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue(privilege)})})
			require.NoError(t, r.reconcile(context.Background(), data))
			granted = false
			require.False(t, invoke(t, r, "create", data, false).HasError())
			require.Len(t, mutations, 2)
			if scope == "SCHEMA" {
				assert.Contains(t, mutations[0], `ON SCHEMA "analytics"."serving"`)
			} else {
				assert.Contains(t, mutations[0], `FOR TABLES IN SCHEMA "serving" DATABASE "analytics"`)
			}
		})
	}
}

// TestSchemaGrantRequiresConsistentScope rejects incompatible schema/scope combinations.
func TestSchemaGrantRequiresConsistentScope(t *testing.T) {
	r := &grantResource{testResourceClient(&catalog{})}
	for _, data := range []grantModel{
		{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA"), SchemaName: types.StringNull()},
		{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("DATABASE"), SchemaName: types.StringValue("serving")},
	} {
		_, _, err := r.read(context.Background(), &data)
		require.ErrorContains(t, err, "schema_name")
	}
	empty := grantTestModel(grantModel{ID: types.StringNull(), DatabaseName: types.StringNull(), SchemaName: types.StringNull(), Role: types.StringNull(), Scope: types.StringNull(), Privileges: types.SetNull(types.StringType)})
	resp := resource.ImportStateResponse{State: testState(t, r, empty)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","role":"readers","scope":"SCHEMA","schema_name":"serving"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var imported grantModel
	require.False(t, resp.State.Get(context.Background(), &imported).HasError())
	assert.Equal(t, "serving", imported.SchemaName.ValueString())
}

// TestRoutineScopesShareCatalogPermissions checks Redshift's shared function/procedure permission scope.
func TestRoutineScopesShareCatalogPermissions(t *testing.T) {
	for _, scope := range []string{"FUNCTIONS", "PROCEDURES"} {
		data := grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Role: types.StringValue("readers"), Scope: types.StringValue(scope)}
		r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
			switch {
			case strings.HasPrefix(sql, "SELECT database_type"):
				return []dataapi.Row{{"database_type": "local"}}, nil
			case strings.HasPrefix(sql, "SELECT role_name"):
				return []dataapi.Row{{"role_name": "readers"}}, nil
			default:
				return []dataapi.Row{{"database_name": "analytics", "schema_name": "serving", "identity_name": "readers", "object_type": "SCHEMA", "privilege_scope": "FUNCTIONS", "privilege_type": "EXECUTE"}}, nil
			}
		}))}
		_, found, err := r.read(context.Background(), &data)
		require.NoError(t, err)
		require.True(t, found)
		assert.Len(t, data.Privileges.Elements(), 1)
	}
}

// TestDatashareScopedGrantLifecycle verifies catalog isolation, emitted SQL, revocation, and imports.
func TestDatashareScopedGrantLifecycle(t *testing.T) {
	for _, scope := range []string{"SCHEMA", "TABLES"} {
		t.Run(scope, func(t *testing.T) {
			privilege, object := "USAGE", `ON SCHEMA "serving"`
			if scope == "TABLES" {
				privilege, object = "SELECT", `FOR TABLES IN SCHEMA "serving"`
			}
			granted := false
			r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, target dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SELECT database_type"):
					return []dataapi.Row{{"database_type": "local"}}, nil
				case strings.HasPrefix(sql, "SELECT share_name"):
					assert.Equal(t, "analytics", target.Database)
					return []dataapi.Row{{"share_name": "share"}}, nil
				case strings.HasPrefix(sql, "SELECT schema_name"):
					return []dataapi.Row{{"schema_name": "serving"}}, nil
				case strings.HasPrefix(sql, "SHOW GRANTS"):
					assert.Equal(t, `SHOW GRANTS ON SCHEMA "serving"`, sql)
					rows := []dataapi.Row{{"database_name": "analytics", "schema_name": "serving", "object_type": "SCHEMA", "identity_name": "other", "privilege_scope": scope, "privilege_type": privilege}}
					if granted {
						rows = append(rows, dataapi.Row{"database_name": "analytics", "schema_name": "serving", "object_type": "SCHEMA", "identity_name": "ds:share", "privilege_scope": scope, "privilege_type": privilege})
					}
					return rows, nil
				case sql == "GRANT "+privilege+" "+object+` TO DATASHARE "share"`:
					granted = true
					return nil, nil
				case sql == "REVOKE "+privilege+" "+object+` FROM DATASHARE "share"`:
					granted = false
					return nil, nil
				default:
					return nil, fmt.Errorf("unexpected SQL %s", sql)
				}
			}))}
			data := grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Datashare: types.StringValue("share"), Scope: types.StringValue(scope), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue(privilege)})})
			require.False(t, invoke(t, r, "create", data, false).HasError())
			assert.True(t, granted)
			require.False(t, invoke(t, r, "delete", data, false).HasError())
			assert.False(t, granted)
			resp := resource.ImportStateResponse{State: testState(t, r, data)}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","datashare":"share","scope":"` + scope + `","schema_name":"serving"}`}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			var imported grantModel
			require.False(t, resp.State.Get(context.Background(), &imported).HasError())
			assert.Equal(t, "share", imported.Datashare.ValueString())
		})
	}
}

// TestDatashareScopedGrantRejectsInvalidTuples preflights unsupported recipients, scopes, and privileges.
func TestDatashareScopedGrantRejectsInvalidTuples(t *testing.T) {
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		if strings.HasPrefix(sql, "SELECT database_type") {
			return []dataapi.Row{{"database_type": "local"}}, nil
		}
		return []dataapi.Row{{"share_name": "share"}}, nil
	}))}
	for _, mode := range []string{"role", "scope", "schema", "privilege", "empty recipient"} {
		data := grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Datashare: types.StringValue("share"), Scope: types.StringValue("SCHEMA"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})}
		switch mode {
		case "role":
			data.Role = types.StringValue("readers")
		case "scope":
			data.Scope = types.StringValue("FUNCTIONS")
		case "schema":
			data.SchemaName = types.StringNull()
		case "privilege":
			data.Privileges = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("CREATE")})
		case "empty recipient":
			data.Datashare = types.StringNull()
		}
		require.Error(t, r.reconcile(context.Background(), data), mode)
	}
}

// TestScopedGrantNormalizesTemporary maps the catalog's TEMP abbreviation to the configured TEMPORARY keyword.
func TestScopedGrantNormalizesTemporary(t *testing.T) {
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT database_type"):
			return []dataapi.Row{{"database_type": "local"}}, nil
		case strings.HasPrefix(sql, "SELECT role_name"):
			return []dataapi.Row{{"role_name": "readers"}}, nil
		default:
			return []dataapi.Row{{"database_name": "analytics", "identity_name": "readers", "object_type": "DATABASE", "privilege_scope": "DATABASE", "privilege_type": "TEMP"}}, nil
		}
	}))}
	data := grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("DATABASE"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("TEMPORARY")})}
	require.NoError(t, r.reconcile(context.Background(), data))
}

// TestScopedGrantMissingSchemaIsNotFound removes grants whose schema was dropped outside Terraform.
func TestScopedGrantMissingSchemaIsNotFound(t *testing.T) {
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT database_type"):
			return []dataapi.Row{{"database_type": "local"}}, nil
		case strings.HasPrefix(sql, "SELECT role_name"):
			return []dataapi.Row{{"role_name": "readers"}}, nil
		case strings.HasPrefix(sql, "SELECT schema_name"):
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected SQL %q", sql)
		}
	}))}
	data := grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA")}
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestScopedGrantCreateRejectsInvalidTupleBeforeState keeps invalid tuples out of state so they cannot block refresh.
func TestScopedGrantCreateRejectsInvalidTupleBeforeState(t *testing.T) {
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))}
	data := grantTestModel(grantModel{ID: types.StringNull(), DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Role: types.StringValue("readers"), Datashare: types.StringNull(), Scope: types.StringValue("DATABASE"), Privileges: types.SetValueMust(types.StringType, nil)})
	plan := testState(t, r, data)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "invalid tuple must not be recorded in state")
}

// TestScopedGrantRejectsUnsupportedDatabases covers unknown bindings and database kinds outside the grant contract.
func TestScopedGrantRejectsUnsupportedDatabases(t *testing.T) {
	for name, databaseType := range map[string]string{"unbound": "", "shared datashare": "shared", "external": "external"} {
		t.Run(name, func(t *testing.T) {
			client := testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, _ string, _ map[string]string) ([]dataapi.Row, error) {
				return []dataapi.Row{{"database_type": databaseType}}, nil
			}))
			if databaseType == "" {
				client.warehouse.value = types.StringUnknown()
			}
			data := grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("DATABASE")}
			if databaseType == "shared" {
				data = grantModel{DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), Datashare: types.StringValue("share"), Scope: types.StringValue("SCHEMA")}
			}
			_, _, err := (&grantResource{client}).read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}
