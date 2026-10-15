package provider

import (
	"context"
	"errors"
	"reflect"
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

// lifecycleKind separates managed objects from permissions, whose deletion revokes only their own tuple.
type lifecycleKind int

const (
	// lifecycleObject is dropped only after memberships and grants on it are gone.
	lifecycleObject lifecycleKind = iota
	// lifecyclePermission revokes itself, so deletion leaves the rest of the catalog in place.
	lifecyclePermission
)

// lifecycleCase describes one resource for the shared error-path tests.
type lifecycleCase struct {
	// name identifies the case; transcript variants refer to it.
	name string
	// kind selects how deletion prepares the catalog.
	kind lifecycleKind
	// new constructs the resource under test.
	new func() resource.Resource
	// model is a representative configuration and state.
	model any
	// setup adjusts every fake catalog first, for example to populate state that fullCatalog lacks.
	setup func(*catalog)
	// absent removes the managed object from a full catalog before creation.
	absent func(*catalog)
	// dependents removes other objects that would otherwise block deletion.
	dependents func(*catalog)
	// prepare adjusts the catalog for one operation, for example drift that update must correct.
	prepare func(c *catalog, operation string)
	// missingRows answers queries against an otherwise empty catalog where the type needs a row to proceed.
	missingRows func(operation, sql string) []dataapi.Row
	// isDeletion recognizes the statements that remove the object; nil uses defaultDeletion.
	isDeletion func(sql string) bool
}

// lifecycleRegistry holds the cases each type's test file registers.
var lifecycleRegistry testRegistry[lifecycleCase]

// registerLifecycleCase declares a resource's shared error-path case from its own test file.
func registerLifecycleCase(test lifecycleCase) bool {
	return lifecycleRegistry.add(test.name, test)
}

// lifecycleCases supplies representative resource models for shared error-path tests in name order.
func lifecycleCases() []lifecycleCase {
	var cases []lifecycleCase
	for _, name := range lifecycleRegistry.keys() {
		cases = append(cases, lifecycleRegistry.entries[name])
	}
	return cases
}

// catalog returns a full fake catalog after the case's setup.
func (test lifecycleCase) catalog() *catalog {
	c := fullCatalog()
	test.applySetup(c)
	return c
}

// applySetup runs the case's catalog setup, if any.
func (test lifecycleCase) applySetup(c *catalog) {
	if test.setup != nil {
		test.setup(c)
	}
}

// applyPrepare runs the case's per-operation adjustment, if any.
func (test lifecycleCase) applyPrepare(c *catalog, operation string) {
	if test.prepare != nil {
		test.prepare(c, operation)
	}
}

// removable clears what blocks deleting the case's object; permissions revoke their own tuple instead.
func (test lifecycleCase) removable(c *catalog) {
	if test.kind == lifecycleObject {
		c.membership = false
		clear(c.privileges)
	}
	if test.dependents != nil {
		test.dependents(c)
	}
}

// deletion reports whether sql removes the case's object.
func (test lifecycleCase) deletion(sql string) bool {
	if test.isDeletion != nil {
		return test.isDeletion(sql)
	}
	return defaultDeletion(sql)
}

// defaultDeletion recognizes removal statements by verb or clause. " DROP USER " covers ALTER GROUP, whose
// removal clause is not a statement verb.
func defaultDeletion(sql string) bool {
	verb, _, _ := strings.Cut(sql, " ")
	switch verb {
	case "DROP", "REVOKE", "DETACH", "RESET":
		return true
	}
	for _, clause := range []string{" REMOVE ", " DETACH ", " RESET ", " DROP USER "} {
		if strings.Contains(sql, clause) {
			return true
		}
	}
	return false
}

// fullCatalog returns a fake catalog in which every lifecycle case's object and parents exist.
func fullCatalog() *catalog {
	c := &catalog{group: true, groupMember: true, role: true, membership: true, identity: true, enabled: true, iamRole: "role-one", database: true, share: true, schema: true, external: true, shareSchema: true, shareTable: true, shareGrant: true, permissions: true, privileges: map[string]bool{"SELECT": true}}
	c.populate()
	return c
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

// TestLifecycleCasesAreRegistered checks that registered cases are complete and name registered resources.
func TestLifecycleCasesAreRegistered(t *testing.T) {
	require.Empty(t, lifecycleRegistry.duplicates, "duplicate lifecycle cases")
	require.NotEmpty(t, lifecycleCases())
	resources, _ := registeredTypeNames()
	for _, test := range lifecycleCases() {
		require.NotNil(t, test.new, test.name)
		require.NotNil(t, test.model, test.name)
		assert.Contains(t, resources, "redshift_"+resourceTypeName(test.new), test.name)
	}
}

// TestDefaultDeletion pins the statements the deletion verification treats as removals.
func TestDefaultDeletion(t *testing.T) {
	for sql, expected := range map[string]bool{
		`DROP ROLE "r"`:                          true,
		`REVOKE SELECT ON TABLE "t" FROM "u"`:    true,
		`ALTER DATASHARE "s" REMOVE SCHEMA "x"`:  true,
		`ALTER GROUP "g" DROP USER "u"`:          true,
		`DETACH RLS POLICY "p" ON "t" FROM "r"`:  true,
		`ALTER USER "u" RESET search_path`:       true,
		`CREATE ROLE "r"`:                        false,
		`ALTER DATASHARE "s" ADD SCHEMA "x"`:     false,
		`GRANT SELECT ON TABLE "t" TO "u"`:       false,
		`SELECT role_name FROM svv_roles`:        false,
		`ALTER IDENTITY PROVIDER "i" DISABLE`:    false,
		`ALTER SCHEMA "s" OWNER TO "removal"`:    false,
		`COMMENT ON SCHEMA "s" IS 'DROP stuff'`:  false,
		`ALTER USER "u" SET search_path TO "x"`:  false,
		`ALTER DATASHARE "s" SET INCLUDENEW = t`: false,
	} {
		assert.Equal(t, expected, defaultDeletion(sql), sql)
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
					c := test.catalog()
					switch {
					case operation == "create" && test.absent != nil:
						test.absent(c)
					case operation == "delete":
						test.removable(c)
					}
					test.applyPrepare(c, operation)
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
				client := queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
					if test.missingRows != nil {
						return test.missingRows(operation, sql), nil
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
			c := test.catalog()
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if test.deletion(sql) {
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
			test.applySetup(c)
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
