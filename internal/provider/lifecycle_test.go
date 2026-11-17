package provider

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invoke executes a Terraform lifecycle method with the model as both plan and state.
func invoke(t *testing.T, r resource.Resource, operation string, model any) diag.Diagnostics {
	t.Helper()

	_, diagnostics := invokeWithState(t, r, operation, model)

	return diagnostics
}

// invokeWithState retains the lifecycle response so shared tests can assert ownership as well as diagnostics.
func invokeWithState(t *testing.T, r resource.Resource, operation string, model any) (tfsdk.State, diag.Diagnostics) {
	t.Helper()

	ctx := context.Background()
	state := testState(t, r, model)
	plan := tfsdk.Plan(state)
	config := tfsdk.Config(state)

	switch operation {
	case "create":
		resp := resource.CreateResponse{State: emptyState(t, r)}
		r.Create(ctx, resource.CreateRequest{Plan: plan, Config: config}, &resp)

		return resp.State, resp.Diagnostics
	case "read":
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)

		return resp.State, resp.Diagnostics
	case "update":
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state, Config: config}, &resp)

		return resp.State, resp.Diagnostics
	case "delete":
		resp := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)

		return resp.State, resp.Diagnostics
	default:
		t.Fatalf("unknown lifecycle operation %q", operation)
		return tfsdk.State{}, nil
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
	missingRows func(operation, sql string) []sqlclient.Row
	// isDeletion recognizes the statements that remove the object; nil uses defaultDeletion.
	isDeletion func(sql string) bool
	// verifyDeletion includes membership and security settings in removal-convergence checks.
	verifyDeletion bool
}

// lifecycleRegistry holds the cases each type's test file registers.
var lifecycleRegistry testRegistry[lifecycleCase]

// lifecycleExemptions documents dedicated lifecycle coverage that needs a different catalog or configuration.
var lifecycleExemptions testRegistry[string]

// registerLifecycleExemption declares dedicated coverage from the resource's own test file.
func registerLifecycleExemption(name, reason string) bool {
	return lifecycleExemptions.add(name, reason)
}

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
func configureTestResource(t *testing.T, r resource.Resource, client sqlclient.Client) {
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
	require.Empty(t, lifecycleExemptions.duplicates, "duplicate lifecycle exemptions")
	require.NotEmpty(t, lifecycleCases())

	resources, _ := registeredTypeNames()
	covered := map[string]bool{}

	for _, test := range lifecycleCases() {
		require.NotNil(t, test.new, test.name)
		require.NotNil(t, test.model, test.name)
		name := "redshift_" + resourceTypeName(test.new)
		assert.Contains(t, resources, name, test.name)
		covered[name] = true
	}

	for _, name := range resources {
		assert.True(t, covered[name] || lifecycleExemptions.entries[name] != "", "%s must register a lifecycle case or document dedicated coverage", name)
	}

	for name, reason := range lifecycleExemptions.entries {
		assert.Contains(t, resources, name, "stale lifecycle exemption")
		assert.False(t, covered[name], "%s has both a lifecycle case and an exemption", name)
		assert.NotEmpty(t, reason, "%s must explain its dedicated coverage", name)
	}
}

// TestLifecycleSQLBoundaryErrors injects failures at every SQL boundary of each lifecycle method.
func TestLifecycleSQLBoundaryErrors(t *testing.T) {
	for _, test := range lifecycleCases() {
		for _, operation := range []string{"create", "read", "update", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				queries := 0
				// Inject each query failure with a fresh catalog, including failures
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
					client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
						calls++
						if calls == failAt {
							return nil, errors.New("injected API failure")
						}

						return c.Query(ctx, target, sql, parameters)
					})
					r := test.new()
					configureTestResource(t, r, client)

					state, diagnostics := invokeWithState(t, r, operation, test.model)
					if failAt == 0 {
						require.False(t, diagnostics.HasError(), "%v", diagnostics)

						queries = calls
					} else {
						require.True(t, diagnostics.HasError(), "query %d failure must be reported", failAt)

						if operation == "read" || operation == "delete" {
							assert.True(t, state.Raw.Equal(testState(t, r, test.model).Raw), "query %d failure must preserve prior state", failAt)
						}

						if operation == "create" && len(c.writes) > 0 {
							require.False(t, state.Raw.IsNull(), "query %d failure after mutation must retain ownership", failAt)
							require.True(t, state.Raw.IsFullyKnown(), "partial-create state must be usable by a later refresh")

							var id types.String
							require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
							require.NotEmpty(t, id.ValueString())
						}
					}
				}
			})
		}
	}
}

// TestLifecyclePartialCreateRecovery refreshes the actual state returned after a successful mutation whose final
// verification failed. A fresh resource instance must recover ownership without repeating the creation statements.
func TestLifecyclePartialCreateRecovery(t *testing.T) {
	for _, test := range lifecycleCases() {
		t.Run(test.name, func(t *testing.T) {
			newCatalog := func() *catalog {
				c := test.catalog()
				if test.absent != nil {
					test.absent(c)
				}

				test.applyPrepare(c, "create")

				return c
			}

			// Locate the final mutation in a successful create, not a hard-coded query number.
			baseline := newCatalog()
			calls, lastMutation := 0, 0
			r := test.new()
			configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				calls++

				rows, err := baseline.Query(ctx, target, sql, parameters)
				if err == nil && !fakeRead(sql) {
					lastMutation = calls
				}

				return rows, err
			}))
			require.False(t, invoke(t, r, "create", test.model).HasError())
			require.Positive(t, lastMutation, "the fixture must exercise creation, not an existing converged object")
			require.Greater(t, calls, lastMutation, "creation must verify its final mutation")

			c := newCatalog()
			calls = 0
			r = test.new()
			configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				calls++
				if calls == lastMutation+1 {
					return nil, errors.New("create verification unavailable")
				}

				return c.Query(ctx, target, sql, parameters)
			}))
			partial, diagnostics := invokeWithState(t, r, "create", test.model)
			require.True(t, diagnostics.HasError())
			require.False(t, partial.Raw.IsNull())
			require.True(t, partial.Raw.IsFullyKnown())

			var id types.String
			require.False(t, partial.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
			require.NotEmpty(t, id.ValueString())

			r = test.new()
			configureTestResource(t, r, c)
			writes := len(c.writes)
			refreshed := resource.ReadResponse{State: partial}
			r.Read(context.Background(), resource.ReadRequest{State: partial}, &refreshed)
			require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)
			require.False(t, refreshed.State.Raw.IsNull(), "refresh must recover the created object")

			var recoveredID types.String
			require.False(t, refreshed.State.GetAttribute(context.Background(), path.Root("id"), &recoveredID).HasError())
			assert.Equal(t, id, recoveredID, "recovery must retain the original identity")
			assert.True(t, refreshed.State.Raw.IsFullyKnown())
			assert.Len(t, c.writes, writes, "recovery must not repeat mutations")
		})
	}
}

// TestLifecycleMissingObjects checks how creation, refresh, update, and deletion handle absent parents.
func TestLifecycleMissingObjects(t *testing.T) {
	for _, test := range lifecycleCases() {
		for _, operation := range []string{"create", "read", "update", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
					if test.missingRows != nil {
						return test.missingRows(operation, sql), nil
					}

					return nil, nil
				})
				r := test.new()
				configureTestResource(t, r, client)
				state, diagnostics := invokeWithState(t, r, operation, test.model)
				assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)

				if operation == "read" {
					assert.True(t, state.Raw.IsNull(), "confirmed absence must remove the resource from state")
				}
			})
		}
	}
}

// TestLifecycleMissingDatabase treats an object whose non-admin database is gone as absent on read and delete, and
// reports a failing database lookup instead of treating it as absence. The admin database skips that lookup, so
// the shared cases run against another database here.
func TestLifecycleMissingDatabase(t *testing.T) {
	for _, test := range lifecycleCases() {
		if !reflect.ValueOf(test.model).FieldByName("Database").IsValid() {
			continue
		}

		local := reflect.New(reflect.TypeOf(test.model)).Elem()
		local.Set(reflect.ValueOf(test.model))
		local.FieldByName("Database").Set(reflect.ValueOf(types.StringValue("warehouse")))

		for _, operation := range []string{"read", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				r := test.new()
				configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
					if test.missingRows != nil {
						return test.missingRows(operation, sql), nil
					}

					return nil, nil
				}))
				state, diagnostics := invokeWithState(t, r, operation, local.Interface())
				assert.False(t, diagnostics.HasError(), "%v", diagnostics)

				if operation == "read" {
					assert.True(t, state.Raw.IsNull(), "a missing parent database must remove the resource from state")
				}

				r = test.new()
				configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
					return nil, errors.New("database lookup failed")
				}))
				state, diagnostics = invokeWithState(t, r, operation, local.Interface())
				require.True(t, diagnostics.HasError())
				assert.Contains(t, diagnostics.Errors()[0].Detail(), "database lookup failed")
				assert.True(t, state.Raw.Equal(testState(t, r, local.Interface()).Raw), "a database lookup failure must preserve prior state")
			})
		}
	}
}

// TestLifecycleImportRoundTrip imports each created object by the JSON identity Create recorded and refreshes it.
func TestLifecycleImportRoundTrip(t *testing.T) {
	for _, test := range lifecycleCases() {
		t.Run(test.name, func(t *testing.T) {
			c := test.catalog()
			if test.absent != nil {
				test.absent(c)
			}

			test.applyPrepare(c, "create")
			r := test.new()
			configureTestResource(t, r, c)
			state, diagnostics := applyOperation(t, r, "create", nil, test.model, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)

			var id types.String
			require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
			diagnostics = importAndRead(t, r, id.ValueString())
			assert.False(t, diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestLifecycleDeletionVerifiesRemoval checks that acknowledged revocations and setting removals actually converge.
func TestLifecycleDeletionVerifiesRemoval(t *testing.T) {
	for _, test := range lifecycleCases() {
		if test.kind != lifecyclePermission && !test.verifyDeletion {
			continue
		}

		t.Run(test.name, func(t *testing.T) {
			c := test.catalog()
			deletions := 0
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if test.deletion(sql) {
					deletions++
					return nil, nil // Simulate an acknowledged write that did not converge.
				}

				return c.Query(ctx, target, sql, parameters)
			})
			r := test.new()
			configureTestResource(t, r, client)
			state, diagnostics := invokeWithState(t, r, "delete", test.model)
			require.Positive(t, deletions, "the test must reach the removal statement")
			require.True(t, diagnostics.HasError(), "%v", diagnostics)
			assert.True(t, state.Raw.Equal(testState(t, r, test.model).Raw), "failed removal must preserve ownership")
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
				diagnostics := invoke(t, r, operation, model)
				require.True(t, diagnostics.HasError(), "%s must reject a different warehouse", operation)
			}

			assert.Empty(t, c.writes)
		})
	}
}
