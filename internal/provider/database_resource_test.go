package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "database", new: newDatabaseResource,
	model:  databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true)},
	absent: func(c *catalog) { c.database = false },
	// Only creation of a shared database needs a visible incoming share.
	missingRows: func(operation, sql string) []dataapi.Row {
		if operation == "create" && strings.HasPrefix(sql, "SELECT consumer_database") {
			return []dataapi.Row{{"consumer_database": ""}}
		}
		return nil
	},
})

var _ = registerReplacementPolicy("redshift_database", map[string]replaceRule{
	"name":             replaceAlways,
	"datashare_arn":    replaceAlways,
	"with_permissions": replaceAlways,
	"owner":            replaceNever,
	"connection_limit": replaceNever,
	"collation":        replaceAlways,
	"isolation_level":  replaceNever,
})

// localDatabaseClient answers the local option reads with Redshift's defaults and passes every other statement,
// SHOW DATABASES above all, to show.
func localDatabaseClient(show queryFunc) queryFunc {
	return func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT u.usename AS owner"):
			return []dataapi.Row{{"owner": "admin", "connection_limit": "UNLIMITED"}}, nil
		case strings.HasPrefix(sql, "SELECT db_collation()"):
			return []dataapi.Row{{"collation": "case_sensitive"}}, nil
		}
		return show(ctx, target, sql, parameters)
	}
}

// TestSharedDatabaseMetadataUsesCompleteJSON verifies permission decoding beyond the truncated legacy view width.
func TestSharedDatabaseMetadataUsesCompleteJSON(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			options := fmt.Sprintf(`{"datashare_name":"source","datashare_producer_account":"123456789012","datashare_producer_namespace":"11111111-2222-3333-4444-555555555555","datashare_producer_region":"eu-central-1","permissions":%t}`, enabled)
			require.Greater(t, strings.Index(options, `"permissions"`), 128)
			r := testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				assert.Equal(t, `SHOW DATABASES LIKE 'analytics'`, sql)
				assert.Nil(t, parameters)
				return []dataapi.Row{{"database_name": "analytics", "database_type": "shared", "parameters": options}}, nil
			}))
			data, found, err := r.databaseMetadata(context.Background(), "analytics")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, enabled, data.WithPermissions.ValueBool())
			assert.Equal(t, "123456789012", data.ProducerAccount.ValueString())
			assert.Equal(t, "11111111-2222-3333-4444-555555555555", data.ProducerNamespace.ValueString())
		})
	}
}

// TestDatabaseMetadataRejectsAmbiguousAndIncompleteRows prevents unknown permission modes from becoming false.
func TestDatabaseMetadataRejectsAmbiguousAndIncompleteRows(t *testing.T) {
	for _, rows := range [][]dataapi.Row{
		{{"database_name": "analytics", "database_type": "shared", "parameters": "broken"}},
		{{"database_name": "analytics", "database_type": "shared", "parameters": `{}`}},
		{{"database_name": "analytics", "database_type": "shared", "parameters": `{"permissions":true}`}},
		{{"database_name": "analytics", "database_type": "local"}, {"database_name": "analytics", "database_type": "local"}},
	} {
		r := testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return rows, nil
		}))
		_, _, err := r.databaseMetadata(context.Background(), "analytics")
		require.Error(t, err)
	}
	r := testResourceClient(localDatabaseClient(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return []dataapi.Row{{"database_name": "analyticsXtest", "database_type": "local"}, {"database_name": "analytics_test", "database_type": "local", "database_isolation_level": "Serializable"}}, nil
	}))
	data, found, err := r.databaseMetadata(context.Background(), "analytics_test")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "analytics_test", data.Name.ValueString())
}

// TestDatabaseCreationErrorsRetainKnownMetadata ensures a successful DDL never leaves unknown ownership fields in state.
func TestDatabaseCreationErrorsRetainKnownMetadata(t *testing.T) {
	for _, mode := range []string{"permission mismatch", "not visible", "read error"} {
		t.Run(mode, func(t *testing.T) {
			r := &databaseResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SELECT consumer_database"):
					return []dataapi.Row{{"consumer_database": ""}}, nil
				case strings.HasPrefix(sql, "CREATE DATABASE"):
					return nil, nil
				default:
					if mode == "read error" {
						return nil, fmt.Errorf("catalog unavailable")
					}
					if mode == "not visible" {
						return nil, nil
					}
					return []dataapi.Row{{"database_name": "analytics", "database_type": "shared", "parameters": `{"datashare_name":"source","datashare_producer_account":"123456789012","datashare_producer_namespace":"11111111-2222-3333-4444-555555555555","permissions":false}`}}, nil
				}
			}))}
			data := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true), DatabaseType: types.StringUnknown(), ShareName: types.StringUnknown(), ProducerAccount: types.StringUnknown(), ProducerNamespace: types.StringUnknown()}
			state := testState(t, r, data)
			response := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &response)
			require.True(t, response.Diagnostics.HasError())
			assert.True(t, response.State.Raw.IsFullyKnown(), "%v", response.State.Raw)
			var observed databaseResourceModel
			require.False(t, response.State.Get(context.Background(), &observed).HasError())
			assert.Equal(t, "shared", observed.DatabaseType.ValueString())
			assert.Equal(t, "123456789012", observed.ProducerAccount.ValueString())
			assert.Equal(t, mode != "permission mismatch", observed.WithPermissions.ValueBool())
		})
	}
}

// TestDatabaseObservesPermissionMode checks shared database permission-mode refresh.
func TestDatabaseObservesPermissionMode(t *testing.T) {
	c := &catalog{database: true}
	r := &databaseResource{testResourceClient(c)}
	data := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN)}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, data.WithPermissions.ValueBool())
	c.permissions = true
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, data.WithPermissions.ValueBool())
}

// TestDatabaseRejectsIncompatibleBinding checks producer identity and database-kind mismatches.
func TestDatabaseRejectsIncompatibleBinding(t *testing.T) {
	for _, change := range []string{"type", "account", "namespace", "share"} {
		t.Run(change, func(t *testing.T) {
			row := dataapi.Row{"database_name": "analytics", "database_type": "shared"}
			options := map[string]any{"datashare_name": "source", "datashare_producer_account": "123456789012", "datashare_producer_namespace": "11111111-2222-3333-4444-555555555555", "permissions": true}
			if change == "type" {
				row["database_type"] = "different"
			} else {
				field := map[string]string{"account": "datashare_producer_account", "namespace": "datashare_producer_namespace", "share": "datashare_name"}[change]
				options[field] = "different"
			}
			encoded, err := json.Marshal(options)
			require.NoError(t, err)
			row["parameters"] = string(encoded)
			writes := 0
			r := &databaseResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				if !strings.HasPrefix(sql, "SHOW") {
					writes++
				}
				return []dataapi.Row{row}, nil
			}))}
			data := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true)}
			resp := resource.DeleteResponse{}
			r.Delete(context.Background(), resource.DeleteRequest{State: testState(t, r, data)}, &resp)
			assert.True(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			assert.Zero(t, writes, "incompatible database must not be dropped")
		})
	}
	for _, value := range []string{"invalid", "arn:aws:s3:eu-central-1:123456789012:bucket", "arn:aws:redshift:eu-central-1:123456789012:datashare:namespace"} {
		_, err := parseShare(value)
		assert.Error(t, err, "invalid share %q", value)
	}
}

// TestDatabaseCreationConditions exercises association discovery and connection failures.
func TestDatabaseCreationConditions(t *testing.T) {
	for _, name := range []string{"invalid ARN", "admin conflict", "unknown target", "already bound", "canceled", "propagation", "without permissions"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &catalog{}
			data := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true)}
			switch name {
			case "invalid ARN":
				data.DatashareARN = types.StringValue("invalid")
			case "admin conflict":
				data.Name = types.StringValue("admin")
			case "unknown target":
			case "already bound":
				c.database = true
			case "canceled":
				cancel()
			case "without permissions":
				data.WithPermissions = types.BoolValue(false)
			}
			polls := 0
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "SELECT consumer_database") {
					polls++
					if name == "canceled" || (name == "propagation" && polls == 1) {
						return nil, nil
					}
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &databaseResource{testResourceClient(client)}
			if name == "unknown target" {
				r.warehouse.value = types.StringUnknown()
			}
			state := testState(t, r, data)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			assert.Equal(t, name != "propagation" && name != "without permissions", resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			if name == "propagation" {
				assert.Equal(t, 2, polls)
			}
			if name == "without permissions" {
				assert.False(t, c.permissions)
			}
		})
	}
	c := &catalog{database: true, permissions: true}
	r := &databaseResource{testResourceClient(c)}
	data := databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue("invalid")}
	_, err := r.read(context.Background(), &data)
	require.Error(t, err)
}

// TestLocalDatabaseLifecycle checks local creation, verification, and unsupported catalog kinds.
func TestLocalDatabaseLifecycle(t *testing.T) {
	for _, caseName := range []string{"create", "create error", "already shared", "missing after create", "read error"} {
		t.Run(caseName, func(t *testing.T) {
			present := caseName == "already shared"
			client := localDatabaseClient(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "SHOW DATABASES") {
					if caseName == "read error" && present {
						return nil, fmt.Errorf("catalog unavailable")
					}
					if !present {
						return nil, nil
					}
					typ := "local"
					if caseName == "already shared" {
						typ = "shared"
					}
					return []dataapi.Row{{"database_name": "warehouse", "database_type": typ, "database_isolation_level": "Snapshot Isolation", "parameters": `{"datashare_name":"source","datashare_producer_account":"123456789012","datashare_producer_namespace":"11111111-2222-3333-4444-555555555555","permissions":true}`}}, nil
				}
				if sql == `CREATE DATABASE "warehouse"` {
					if caseName == "create error" {
						return nil, fmt.Errorf("creation failed")
					}
					present = caseName != "missing after create"
					return nil, nil
				}
				return nil, fmt.Errorf("unexpected SQL %q", sql)
			})
			r := &databaseResource{testResourceClient(client)}
			data := databaseModel{Name: types.StringValue("warehouse"), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true)}
			if caseName == "already shared" {
				_, err := r.read(context.Background(), &data)
				require.ErrorContains(t, err, "not a local database")
				return
			}
			state := testState(t, r, data)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			assert.Equal(t, caseName != "create", resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			if caseName == "create" {
				assert.True(t, present)
			}
		})
	}
}

// TestDatabaseImportAllowsLocalDatabase checks imports without a datashare binding.
func TestDatabaseImportAllowsLocalDatabase(t *testing.T) {
	r := &databaseResource{}
	empty := databaseModel{ID: types.StringNull(), Name: types.StringNull(), DatashareARN: types.StringNull(), WithPermissions: types.BoolNull()}
	resp := resource.ImportStateResponse{State: testState(t, r, empty)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","name":"warehouse"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var data databaseResourceModel
	require.False(t, resp.State.Get(context.Background(), &data).HasError())
	assert.True(t, data.DatashareARN.IsNull())
	resp = resource.ImportStateResponse{State: testState(t, r, empty)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "invalid JSON"}, &resp)
	assert.True(t, resp.Diagnostics.HasError())
}

// TestLocalDatabaseIgnoresPermissionMode checks that shared-only options do not affect local reads.
func TestLocalDatabaseIgnoresPermissionMode(t *testing.T) {
	r := &databaseResource{testResourceClient(localDatabaseClient(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return []dataapi.Row{{"database_name": "warehouse", "database_type": "local", "database_isolation_level": "Snapshot Isolation"}}, nil
	}))}
	data := databaseModel{Name: types.StringValue("warehouse"), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(false)}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, data.WithPermissions.ValueBool())
	data.WithPermissions = types.BoolUnknown()
	_, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, data.WithPermissions.ValueBool())
}

// TestWithPermissionsReplacesOnlySharedDatabases keeps local databases in place when the ignored argument changes.
func TestWithPermissionsReplacesOnlySharedDatabases(t *testing.T) {
	for name, arn := range map[string]types.String{"local": types.StringNull(), "shared": types.StringValue("arn:aws:redshift:eu-central-1:123456789012:datashare:11111111-2222-3333-4444-555555555555/source")} {
		t.Run(name, func(t *testing.T) {
			state := testState(t, newDatabaseResource(), &databaseModel{
				ID: types.StringValue("{}"), Name: types.StringValue("analytics"), DatashareARN: arn, WithPermissions: types.BoolValue(true),
				DatabaseType: types.StringNull(), ShareName: types.StringNull(), ProducerAccount: types.StringNull(), ProducerNamespace: types.StringNull(),
			})
			request := planmodifier.BoolRequest{State: state, StateValue: types.BoolValue(true), PlanValue: types.BoolValue(false), ConfigValue: types.BoolValue(false)}
			var response boolplanmodifier.RequiresReplaceIfFuncResponse
			sharedDatabaseReplacement(context.Background(), request, &response)
			assert.Equal(t, !arn.IsNull(), response.RequiresReplace)
		})
	}
}

// TestDatabaseMetadataFindsWildcardNames reads names holding LIKE metacharacters through a fake that matches the
// pattern the way Redshift does, with backslash as the escape character.
func TestDatabaseMetadataFindsWildcardNames(t *testing.T) {
	databases := []string{`a\b`, "ab", "a_b", "a%b", "axb"}
	r := testResourceClient(localDatabaseClient(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		quoted, found := strings.CutPrefix(sql, "SHOW DATABASES LIKE ")
		require.True(t, found, sql)
		value := strings.NewReplacer(`''`, `'`, `\\`, `\`).Replace(strings.Trim(quoted, "'"))
		var expression strings.Builder
		for i := 0; i < len(value); i++ {
			switch {
			case value[i] == '\\' && i+1 < len(value):
				i++
				expression.WriteString(regexp.QuoteMeta(value[i : i+1]))
			case value[i] == '%':
				expression.WriteString(".*")
			case value[i] == '_':
				expression.WriteString(".")
			default:
				expression.WriteString(regexp.QuoteMeta(value[i : i+1]))
			}
		}
		pattern := regexp.MustCompile("^" + expression.String() + "$")
		var rows []dataapi.Row
		for _, name := range databases {
			if pattern.MatchString(name) {
				rows = append(rows, dataapi.Row{"database_name": name, "database_type": "local", "database_isolation_level": "Serializable"})
			}
		}
		return rows, nil
	}))
	for _, name := range databases {
		t.Run(name, func(t *testing.T) {
			_, found, err := r.databaseMetadata(context.Background(), name)
			require.NoError(t, err)
			assert.True(t, found)
		})
	}
}
