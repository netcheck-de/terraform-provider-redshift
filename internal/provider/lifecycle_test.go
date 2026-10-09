package provider

import (
	"context"
	"errors"
	"reflect"
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
	case "delete":
		resp := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		return resp.Diagnostics
	default:
		t.Fatalf("unknown lifecycle operation %q", operation)
		return nil
	}
}

// lifecycleCase describes one resource for the shared error-path tests.
type lifecycleCase struct {
	name  string
	new   func() resource.Resource
	model any
	// absent removes the managed object from a full catalog before creation.
	absent func(*catalog)
	// dependents removes other objects that would otherwise block deletion.
	dependents func(*catalog)
}

// lifecycleCases supplies representative resource models for shared error-path tests.
func lifecycleCases() []lifecycleCase {
	return []lifecycleCase{
		{name: "group", new: newGroupResource, model: groupModel{Name: types.StringValue("readers")}, absent: func(c *catalog) { c.group = false }},
		{name: "group membership", new: newGroupMembershipResource, model: groupMembershipModel{Group: types.StringValue("readers"), User: types.StringValue("grafana")}, absent: func(c *catalog) { c.groupMember = false }},
		{name: "role", new: newRoleResource, model: roleModel{Name: types.StringValue("example:readers")}, absent: func(c *catalog) { c.role = false }},
		{name: "membership", new: newRoleGrantResource, model: roleGrantModel{Role: types.StringValue("sys:dba"), ToRole: types.StringValue("example:readers"), ToUser: types.StringNull()}},
		{name: "database", new: newDatabaseResource, model: databaseModel{Name: types.StringValue("analytics"), DatashareARN: types.StringValue(shareARN), WithPermissions: types.BoolValue(true)}, absent: func(c *catalog) { c.database = false }},
		{name: "datashare", new: newDatashareResource, model: datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer"), PublicAccessible: types.BoolValue(false)}, absent: func(c *catalog) { c.share = false }, dependents: func(c *catalog) { c.shareSchema, c.shareGrant = false, false }},
		{name: "schema", new: newSchemaResource, model: schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringValue("admin")}, absent: func(c *catalog) { c.schema = false }},
		{name: "external schema", new: newExternalSchemaResource, model: externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("example_glue"), IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), RefreshRevision: types.StringNull()}, absent: func(c *catalog) { c.external = false }},
		{name: "share schema", new: newDatashareSchemaResource, model: datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), IncludeNew: types.BoolValue(false)}, absent: func(c *catalog) { c.shareSchema = false }, dependents: func(c *catalog) { c.shareTable = false }},
		{name: "share table", new: newDatashareTableResource, model: datashareTableModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Table: types.StringValue("table")}, absent: func(c *catalog) { c.shareTable = false }},
		{name: "share grant", new: newDatashareGrantResource, model: datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012")}, absent: func(c *catalog) { c.shareGrant = false }},
		{name: "identity", new: newIdentityProviderResource, model: identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}, absent: func(c *catalog) { c.identity = false }, dependents: func(c *catalog) { c.role = false }},
		{name: "grant", new: newGrantResource, model: grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("example:readers"), Scope: types.StringValue("TABLES"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, absent: func(c *catalog) { clear(c.privileges) }},
	}
}

// fullCatalog returns a fake catalog in which every lifecycle case's object and parents exist.
func fullCatalog() *catalog {
	return &catalog{group: true, groupMember: true, role: true, membership: true, identity: true, enabled: true, iamRole: "role-one", database: true, share: true, schema: true, external: true, shareSchema: true, shareTable: true, shareGrant: true, permissions: true, privileges: map[string]bool{"SELECT": true}}
}

// configureTestResource binds a resource to the test warehouse and the supplied SQL client.
func configureTestResource(t *testing.T, r resource.Resource, client dataapi.Client) {
	t.Helper()
	var configured resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerData{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin")}}, &configured)
	require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)
}

// withID returns a copy of a resource model with its ID field replaced.
func withID(model any, id types.String) any {
	value := reflect.New(reflect.TypeOf(model)).Elem()
	value.Set(reflect.ValueOf(model))
	value.FieldByName("ID").Set(reflect.ValueOf(id))
	return value.Interface()
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
					c := fullCatalog()
					switch {
					case operation == "create" && test.absent != nil:
						test.absent(c)
					case operation == "delete" && test.name != "grant":
						// Grants revoke their own privileges; other objects need their dependents gone first.
						c.membership = false
						clear(c.privileges)
						if test.dependents != nil {
							test.dependents(c)
						}
					case operation == "update" && test.name == "grant":
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
					configureTestResource(t, r, client)
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
				configureTestResource(t, r, client)
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
			c := fullCatalog()
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "DROP ") || strings.HasPrefix(sql, "REVOKE ") || strings.Contains(sql, " REMOVE ") || strings.Contains(sql, " DROP USER ") {
					return nil, nil // Simulate an acknowledged write that did not converge.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := test.new()
			configureTestResource(t, r, client)
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
			configureTestResource(t, r, c)
			model := withID(test.model, types.StringValue(`{"workgroup_name":"other","database":"admin"}`))
			for _, operation := range []string{"read", "update", "delete"} {
				diagnostics := invoke(t, r, operation, model, false)
				require.True(t, diagnostics.HasError(), "%s must reject a different warehouse", operation)
			}
			assert.Empty(t, c.writes)
		})
	}
}
