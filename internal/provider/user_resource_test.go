package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grafanaUser supplies a non-administrative user model for lifecycle tests.
func grafanaUser() userModel {
	return userModel{Name: types.StringValue("grafana"), Password: types.StringNull(), PasswordVersion: types.Int64Value(0), Superuser: types.BoolValue(false), CreateDB: types.BoolValue(false)}
}

// runUser invokes user lifecycle operations with a separate write-only configuration secret.
func runUser(t *testing.T, r *userResource, operation string, plan, previous userModel, secret string) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	planState := testState(t, r, plan)
	priorState := testState(t, r, previous)
	config := plan
	config.Password = types.StringNull()
	if secret != "" {
		config.Password = types.StringValue(secret)
	}
	configured := testState(t, r, config)
	switch operation {
	case "create":
		resp := resource.CreateResponse{State: tfsdk.State{Schema: planState.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planState), Config: tfsdk.Config(configured)}, &resp)
		return resp.Diagnostics
	case "update":
		resp := resource.UpdateResponse{State: priorState}
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan(planState), State: priorState, Config: tfsdk.Config(configured)}, &resp)
		return resp.Diagnostics
	case "read":
		resp := resource.ReadResponse{State: priorState}
		r.Read(ctx, resource.ReadRequest{State: priorState}, &resp)
		return resp.Diagnostics
	default:
		resp := resource.DeleteResponse{State: priorState}
		r.Delete(ctx, resource.DeleteRequest{State: priorState}, &resp)
		return resp.Diagnostics
	}
}

// TestUserCreatesAndRotatesPassword checks version-controlled password updates without state persistence.
func TestUserCreatesAndRotatesPassword(t *testing.T) {
	c := &catalog{}
	r := &userResource{testResourceClient(c)}
	data := grafanaUser()
	require.False(t, runUser(t, r, "create", data, data, "InitialPass123").HasError())
	assert.True(t, c.user)
	assert.Contains(t, c.writes[0], "PASSWORD 'InitialPass123'")
	data.ID = r.identity("admin", map[string]string{"name": "grafana"})
	previous := data
	data.PasswordVersion = types.Int64Value(1)
	require.False(t, runUser(t, r, "update", data, previous, "RotatedPass456").HasError())
	assert.Contains(t, c.writes, `ALTER USER "grafana" PASSWORD 'RotatedPass456'`)
	previous = data
	data.Superuser, data.CreateDB = types.BoolValue(true), types.BoolValue(true)
	require.False(t, runUser(t, r, "update", data, previous, "").HasError())
	assert.True(t, c.superuser)
	assert.True(t, c.createDB)
	previous = data
	data.Superuser, data.CreateDB = types.BoolValue(false), types.BoolValue(false)
	require.False(t, runUser(t, r, "update", data, previous, "").HasError())
	assert.False(t, c.superuser)
	assert.False(t, c.createDB)
	require.False(t, runUser(t, r, "delete", data, data, "").HasError())
	assert.False(t, c.user)
}

// TestUserImportDoesNotRotatePassword ensures imported users are not assigned new passwords implicitly.
func TestUserImportDoesNotRotatePassword(t *testing.T) {
	c := &catalog{user: true}
	r := &userResource{testResourceClient(c)}
	data := grafanaUser()
	data.ID = r.identity("admin", map[string]string{"name": "grafana"})
	previous := data
	previous.PasswordVersion = types.Int64Null()
	require.False(t, runUser(t, r, "update", data, previous, "").HasError())
	assert.Empty(t, c.writes)
	// Terraform schedules a write-only resource update during declarative import.
	// Supplying the existing credential at version zero must still issue no SQL.
	require.False(t, runUser(t, r, "update", data, data, "ExistingPass123").HasError())
	assert.Empty(t, c.writes)
}

// TestUserRejectsMissingPassword checks required creation secrets.
func TestUserRejectsMissingPassword(t *testing.T) {
	r := &userResource{testResourceClient(&catalog{})}
	data := grafanaUser()
	assert.True(t, runUser(t, r, "create", data, data, "").HasError())
	c := &catalog{user: true}
	r = &userResource{testResourceClient(c)}
	data.ID = r.identity("admin", map[string]string{"name": "grafana"})
	previous := data
	data.PasswordVersion = types.Int64Value(1)
	assert.True(t, runUser(t, r, "update", data, previous, "").HasError())
	assert.Empty(t, c.writes)
}

// TestUserCatalogReadRejectsInvalidRows rejects malformed capability values and ambiguous users.
func TestUserCatalogReadRejectsInvalidRows(t *testing.T) {
	for _, rows := range [][]dataapi.Row{
		{{"usename": "grafana", "usesuper": "invalid", "usecreatedb": "false"}},
		{{"usename": "grafana", "usesuper": "false", "usecreatedb": "invalid"}},
		{{"usename": "grafana", "usesuper": "false", "usecreatedb": "false"}, {"usename": "grafana"}},
	} {
		r := &userResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return rows, nil
		}))}
		data := grafanaUser()
		_, err := r.read(context.Background(), &data)
		require.Error(t, err)
	}
}

// TestUserReadRejectsOtherWarehouse prevents reading a same-named user through a changed binding.
func TestUserReadRejectsOtherWarehouse(t *testing.T) {
	c := &catalog{user: true}
	r := &userResource{testResourceClient(c)}
	data := grafanaUser()
	data.ID = types.StringValue(`{"workgroup_name":"other","database":"admin","name":"grafana"}`)
	_, err := r.read(context.Background(), &data)
	require.ErrorContains(t, err, "provider warehouse")
	assert.Empty(t, c.writes)
}

// TestUserCreateWithAdministrativeFlags checks creation SQL for superuser and database privileges.
func TestUserCreateWithAdministrativeFlags(t *testing.T) {
	c := &catalog{}
	r := &userResource{testResourceClient(c)}
	data := grafanaUser()
	data.Superuser, data.CreateDB = types.BoolValue(true), types.BoolValue(true)
	require.False(t, runUser(t, r, "create", data, data, "AdminPass123").HasError())
	assert.True(t, c.superuser)
	assert.True(t, c.createDB)
}

// TestUserOperationsReportCatalogErrors verifies catalog failures reach Terraform diagnostics.
func TestUserOperationsReportCatalogErrors(t *testing.T) {
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			for failAt := 1; failAt <= 3; failAt++ {
				c := &catalog{user: operation != "create"}
				current := 0
				client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
					current++
					if current == failAt {
						return nil, errors.New("injected API error")
					}
					return c.Query(ctx, target, sql, parameters)
				})
				r := &userResource{testResourceClient(client)}
				data := grafanaUser()
				if operation != "create" {
					data.ID = r.identity("admin", map[string]string{"name": "grafana"})
				}
				planned, prior := data, data
				if operation == "update" {
					planned.PasswordVersion = types.Int64Value(1)
				}
				diagnostics := runUser(t, r, operation, planned, prior, "NextPass123")
				assert.Equal(t, current >= failAt, diagnostics.HasError(), "failure at query %d", failAt)
			}
		})
	}
}

// TestUserUpdateDetectsMismatchedBindingAndPrivileges checks update ownership and convergence.
func TestUserUpdateDetectsMismatchedBindingAndPrivileges(t *testing.T) {
	c := &catalog{user: true}
	r := &userResource{testResourceClient(c)}
	data := grafanaUser()
	data.ID = types.StringValue(`{"workgroup_name":"other","database":"admin","name":"grafana"}`)
	assert.True(t, runUser(t, r, "update", data, data, "").HasError())
	assert.Empty(t, c.writes)
	data.ID = r.identity("admin", map[string]string{"name": "grafana"})
	previous := data
	data.Superuser = types.BoolValue(true)
	client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
		if strings.HasPrefix(sql, "ALTER USER") {
			return nil, nil
		}
		return c.Query(ctx, target, sql, parameters)
	})
	r.client = client
	assert.True(t, runUser(t, r, "update", data, previous, "").HasError())
}

// TestUserLifecycleErrorPaths injects SQL failures and missing user states across lifecycle operations.
func TestUserLifecycleErrorPaths(t *testing.T) {
	for _, operation := range []string{"create", "read", "update", "delete"} {
		r := &userResource{testResourceClient(&catalog{user: true})}
		data := grafanaUser()
		require.True(t, invoke(t, r, operation, data, true).HasError())
	}
	for _, operation := range []string{"read", "delete"} {
		r := &userResource{testResourceClient(&catalog{})}
		data := grafanaUser()
		data.ID = r.identity("admin", map[string]string{"name": "grafana"})
		assert.False(t, runUser(t, r, operation, data, data, "").HasError())
	}
	for _, stage := range []string{"create", "create verification", "create missing", "create mismatch", "delete remains"} {
		t.Run(stage, func(t *testing.T) {
			c := &catalog{}
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, params map[string]string) ([]dataapi.Row, error) {
				if stage == "create" && strings.HasPrefix(sql, "CREATE USER") {
					return nil, errors.New("create denied")
				}
				if stage == "create verification" && c.user && strings.HasPrefix(sql, "SELECT usename") {
					return nil, errors.New("catalog unavailable")
				}
				if stage == "create missing" && strings.HasPrefix(sql, "SELECT usename") {
					return nil, nil
				}
				if stage == "create mismatch" && strings.HasPrefix(sql, "SELECT usename") {
					return []dataapi.Row{{"usename": "grafana", "usesuper": "false", "usecreatedb": "false"}}, nil
				}
				if stage == "delete remains" && strings.HasPrefix(sql, "DROP USER") {
					return nil, nil
				}
				return c.Query(ctx, target, sql, params)
			})
			r := &userResource{testResourceClient(client)}
			data := grafanaUser()
			if stage == "create mismatch" {
				data.Superuser = types.BoolValue(true)
			}
			if stage == "delete remains" {
				c.user = true
				data.ID = r.identity("admin", map[string]string{"name": "grafana"})
				assert.True(t, runUser(t, r, "delete", data, data, "").HasError())
			} else {
				assert.True(t, runUser(t, r, "create", data, data, "InitialPass123").HasError())
			}
		})
	}
}

// TestUserUpdateFailures checks password-version, configuration, and SQL update failure branches.
func TestUserUpdateFailures(t *testing.T) {
	for _, stage := range []string{"rotation", "privilege", "catalog", "missing", "mismatch", "invalid config"} {
		t.Run(stage, func(t *testing.T) {
			c := &catalog{user: true}
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, params map[string]string) ([]dataapi.Row, error) {
				if stage == "rotation" && strings.Contains(sql, " PASSWORD ") {
					return nil, errors.New("rotation denied")
				}
				if stage == "privilege" && strings.HasSuffix(sql, " CREATEUSER") {
					return nil, errors.New("ALTER denied")
				}
				if stage == "catalog" && strings.HasPrefix(sql, "SELECT usename") {
					return nil, errors.New("catalog unavailable")
				}
				if stage == "missing" && strings.HasPrefix(sql, "SELECT usename") {
					return nil, nil
				}
				if stage == "mismatch" && strings.HasPrefix(sql, "ALTER USER") {
					return nil, nil
				}
				return c.Query(ctx, target, sql, params)
			})
			r := &userResource{testResourceClient(client)}
			previous := grafanaUser()
			previous.ID = r.identity("admin", map[string]string{"name": "grafana"})
			planned := previous
			if stage == "rotation" || stage == "invalid config" {
				planned.PasswordVersion = types.Int64Value(1)
			} else {
				planned.Superuser = types.BoolValue(true)
			}
			if stage == "invalid config" {
				state := testState(t, r, previous)
				plan := testState(t, r, planned)
				resp := resource.UpdateResponse{State: state}
				r.Update(context.Background(), resource.UpdateRequest{
					State: state, Plan: tfsdk.Plan(plan),
					Config: tfsdk.Config{Schema: plan.Schema, Raw: tftypes.NewValue(tftypes.String, "invalid config")},
				}, &resp)
				assert.True(t, resp.Diagnostics.HasError())
				return
			}
			assert.True(t, runUser(t, r, "update", planned, previous, "RotatedPass123").HasError())
		})
	}
}
