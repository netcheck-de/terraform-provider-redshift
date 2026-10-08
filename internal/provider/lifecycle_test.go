package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invoke executes a Terraform lifecycle method with typed or deliberately invalid state.
func invoke(t *testing.T, r resource.Resource, operation string, model any, invalid bool) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	state := testState(t, r, model)
	if invalid {
		state.Raw = tftypes.NewValue(tftypes.String, "invalid model")
	}
	plan := tfsdk.Plan(state)
	switch operation {
	case "create":
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
		return resp.Diagnostics
	case "read":
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)
		return resp.Diagnostics
	case "update":
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &resp)
		return resp.Diagnostics
	default:
		resp := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		return resp.Diagnostics
	}
}

// lifecycleCases supplies representative resource models for shared error-path tests.
func lifecycleCases() []struct {
	name  string
	new   func() resource.Resource
	model any
} {
	return []struct {
		name  string
		new   func() resource.Resource
		model any
	}{
		{"group", newGroupResource, groupModel{Name: types.StringValue("readers")}},
		{"group membership", newGroupMembershipResource, groupMembershipModel{Group: types.StringValue("readers"), User: types.StringValue("grafana")}},
		{"role", newRoleResource, roleModel{Name: types.StringValue("example:readers")}},
		{"membership", newRoleGrantResource, roleGrantModel{Role: types.StringValue("sys:dba"), ToRole: types.StringValue("example:readers"), ToUser: types.StringNull()}},
		{"database", newDatabaseResource, databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true)}},
		{"datashare", newDatashareResource, datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer"), PublicAccessible: types.BoolValue(false)}},
		{"schema", newSchemaResource, schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringValue("admin")}},
		{"external schema", newExternalSchemaResource, externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("example_glue"), IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), RefreshRevision: types.StringNull()}},
		{"share schema", newDatashareSchemaResource, datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), IncludeNew: types.BoolValue(false)}},
		{"share table", newDatashareTableResource, datashareTableModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Table: types.StringValue("table")}},
		{"share grant", newDatashareGrantResource, datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012")}},
		{"identity", newIdentityProviderResource, identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}},
		{"grant", newGrantResource, grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("example:readers"), Scope: types.StringValue("TABLES"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}},
	}
}

// TestLifecycleDiagnosticsAndRetries injects failures at every SQL boundary of each lifecycle method.
func TestLifecycleDiagnosticsAndRetries(t *testing.T) {
	for _, test := range lifecycleCases() {
		for _, operation := range []string{"create", "read", "update", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				r := test.new()
				diagnostics := invoke(t, r, operation, test.model, true)
				require.True(t, diagnostics.HasError(), "invalid request must not reach SQL")
				queries := 0
				// Retry each query failure point with a fresh catalog, including failures
				// after DDL has succeeded but before verification completes.
				for failAt := 0; failAt <= queries; failAt++ {
					c := &catalog{group: true, groupMember: true, role: true, membership: true, identity: true, enabled: true, iamRole: "role-one", database: true, share: true, schema: true, external: true, shareSchema: true, shareTable: true, shareGrant: true, permissions: true, privileges: map[string]bool{"SELECT": true}}
					if operation == "create" {
						switch test.name {
						case "group":
							c.group = false
						case "group membership":
							c.groupMember = false
						case "role":
							c.role = false
						case "database":
							c.database = false
						case "datashare":
							c.share = false
						case "schema":
							c.schema = false
						case "external schema":
							c.external = false
						case "share schema":
							c.shareSchema = false
						case "share table":
							c.shareTable = false
						case "share grant":
							c.shareGrant = false
						case "identity":
							c.identity = false
						case "grant":
							clear(c.privileges)
						}
					}
					if operation == "delete" && test.name != "grant" {
						c.membership = false
						clear(c.privileges)
						if test.name == "identity" {
							c.role = false
						}
						if test.name == "share schema" {
							c.shareTable = false
						}
						if test.name == "datashare" {
							c.shareSchema, c.shareGrant = false, false
						}
					}
					if operation == "update" && test.name == "grant" {
						c.privileges = map[string]bool{"INSERT": true}
					}
					calls := 0
					client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
						calls++
						if calls == failAt {
							return nil, errors.New("injected API failure")
						}
						return c.Query(ctx, target, sql, parameters)
					})
					r = test.new()
					var configured resource.ConfigureResponse
					r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}}, &configured)
					require.False(t, configured.Diagnostics.HasError())
					diagnostics = invoke(t, r, operation, test.model, false)
					if failAt == 0 {
						require.False(t, diagnostics.HasError(), "%v", diagnostics)
						queries = calls
					} else {
						require.True(t, diagnostics.HasError(), "query %d failure must be reported", failAt)
					}
				}
			})
		}
	}
}

// TestLifecycleMissingObjects checks how creation, refresh, update, and deletion handle absent parents.
func TestLifecycleMissingObjects(t *testing.T) {
	for _, test := range lifecycleCases() {
		for _, operation := range []string{"create", "read", "update", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				// Only creation of a shared database needs a visible incoming share.
				client := queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
					if test.name == "database" && operation == "create" && strings.HasPrefix(sql, "SELECT consumer_database") {
						return []dataapi.Row{{"consumer_database": ""}}, nil
					}
					return nil, nil
				})
				r := test.new()
				var configured resource.ConfigureResponse
				r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}}, &configured)
				diagnostics := invoke(t, r, operation, test.model, false)
				assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)
			})
		}
	}
}

// TestDeletionVerifiesRemoval checks that acknowledged but ineffective deletion fails verification.
func TestDeletionVerifiesRemoval(t *testing.T) {
	for _, test := range lifecycleCases() {
		t.Run(test.name, func(t *testing.T) {
			c := &catalog{group: true, groupMember: true, role: true, membership: true, identity: true, enabled: true, iamRole: "role-one", database: true, share: true, schema: true, external: true, shareSchema: true, shareTable: true, shareGrant: true, permissions: true, privileges: map[string]bool{"SELECT": true}}
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "DROP ") || strings.HasPrefix(sql, "REVOKE ") || strings.Contains(sql, " REMOVE ") || strings.Contains(sql, " DROP USER ") {
					return nil, nil // Simulate an acknowledged write that did not converge.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := test.new()
			var configured resource.ConfigureResponse
			r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}}, &configured)
			diagnostics := invoke(t, r, "delete", test.model, false)
			assert.True(t, diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestProviderBindingCannotAdoptAnotherWarehouse rejects cross-warehouse resource state.
func TestProviderBindingCannotAdoptAnotherWarehouse(t *testing.T) {
	for _, test := range lifecycleCases() {
		t.Run(test.name, func(t *testing.T) {
			c := &catalog{role: true, membership: true, identity: true, enabled: true, iamRole: "role-one", database: true, share: true, permissions: true, privileges: map[string]bool{"SELECT": true}}
			r := test.new()
			var configured resource.ConfigureResponse
			r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: c, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}}, &configured)
			require.False(t, configured.Diagnostics.HasError())
			id := types.StringValue(`{"workgroup_name":"other","database":"admin"}`)
			var model any
			switch value := test.model.(type) {
			case groupModel:
				value.ID = id
				model = value
			case groupMembershipModel:
				value.ID = id
				model = value
			case roleModel:
				value.ID = id
				model = value
			case roleGrantModel:
				value.ID = id
				model = value
			case databaseModel:
				value.ID = id
				model = value
			case datashareModel:
				value.ID = id
				model = value
			case schemaModel:
				value.ID = id
				model = value
			case externalSchemaModel:
				value.ID = id
				model = value
			case datashareSchemaModel:
				value.ID = id
				model = value
			case datashareTableModel:
				value.ID = id
				model = value
			case datashareGrantModel:
				value.ID = id
				model = value
			case identityProviderModel:
				value.ID = id
				model = value
			case grantModel:
				value.ID = id
				model = value
			}
			for _, operation := range []string{"read", "update", "delete"} {
				diagnostics := invoke(t, r, operation, model, false)
				require.True(t, diagnostics.HasError(), "%s must reject a different warehouse", operation)
			}
			assert.Empty(t, c.writes)
		})
	}
}
