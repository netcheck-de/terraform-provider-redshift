package provider

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timedResources are the resource types that declare the timeouts block.
var timedResources = []string{"redshift_database", "redshift_materialized_view", "redshift_table"}

// TestTimeoutsBlockResources pins which resources declare the timeouts block, so adding it elsewhere is deliberate.
func TestTimeoutsBlockResources(t *testing.T) {
	var timed []string
	for _, factory := range New("test")().Resources(context.Background()) {
		r := factory()
		var response resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &response)
		if _, ok := response.Schema.Blocks[timeoutsBlockName]; ok {
			timed = append(timed, "redshift_"+resourceTypeName(factory))
		}
	}
	slices.Sort(timed)
	assert.Equal(t, timedResources, timed)
}

// TestOperationTimeoutsBlock checks the block's operations, descriptions, and duration validation.
func TestOperationTimeoutsBlock(t *testing.T) {
	ctx := context.Background()
	block, ok := operationTimeoutsBlock(ctx, " Extra note.").(schema.SingleNestedBlock)
	require.True(t, ok)
	assert.NotEmpty(t, block.MarkdownDescription)
	assert.IsType(t, timeouts.Type{}, block.Type(), "the custom type lets timeouts.Value read the durations")
	assert.ElementsMatch(t, []string{"create", "update", "delete"}, slices.Collect(maps.Keys(block.Attributes)), "reads never write, so they need no timeout")
	assert.True(t, strings.HasSuffix(block.Attributes["create"].GetMarkdownDescription(), " Extra note."))
	for name, attribute := range block.Attributes {
		value := attribute.(schema.StringAttribute)
		assert.True(t, value.Optional, name)
		assert.NotEmpty(t, value.MarkdownDescription, name)
		require.Len(t, value.Validators, 1, name)
		for input, valid := range map[string]bool{"30s": true, "1h30m": true, "0s": false, "-5m": false, "10": false, "soon": false} {
			var response validator.StringResponse
			value.Validators[0].ValidateString(ctx, validator.StringRequest{Path: path.Root(name), ConfigValue: types.StringValue(input)}, &response)
			assert.Equal(t, !valid, response.Diagnostics.HasError(), "%s = %q", name, input)
		}
	}
}

// TestBoundOperation leaves the context unbounded without a timeout, bounds it with one, and names an expired
// timeout only when the operation failed.
func TestBoundOperation(t *testing.T) {
	configured := func(timeout time.Duration) func(context.Context, time.Duration) (time.Duration, diag.Diagnostics) {
		return func(context.Context, time.Duration) (time.Duration, diag.Diagnostics) { return timeout, nil }
	}
	var diagnostics diag.Diagnostics
	ctx, done := boundOperation(context.Background(), "create", configured(0), &diagnostics)
	_, bounded := ctx.Deadline()
	assert.False(t, bounded, "an unset timeout adds no deadline")
	done()

	before := time.Now()
	ctx, done = boundOperation(context.Background(), "update", configured(time.Hour), &diagnostics)
	deadline, bounded := ctx.Deadline()
	require.True(t, bounded)
	assert.WithinDuration(t, before.Add(time.Hour), deadline, time.Minute)
	done()
	require.ErrorIs(t, ctx.Err(), context.Canceled, "done releases the deadline")
	assert.False(t, diagnostics.HasError())

	ctx, done = boundOperation(context.Background(), "delete", configured(time.Millisecond), &diagnostics)
	<-ctx.Done()
	done()
	assert.False(t, diagnostics.HasError(), "an expired deadline of a successful operation adds no error")

	ctx, done = boundOperation(context.Background(), "delete", configured(time.Millisecond), &diagnostics)
	<-ctx.Done()
	diagnostics.AddError("Delete table", ctx.Err().Error())
	done()
	require.Len(t, diagnostics.Errors(), 2)
	assert.Equal(t, "Operation timed out", diagnostics.Errors()[1].Summary())
	assert.Contains(t, diagnostics.Errors()[1].Detail(), "delete timeout of 1ms")

	var invalid diag.Diagnostics
	_, done = boundOperation(context.Background(), "create", func(context.Context, time.Duration) (time.Duration, diag.Diagnostics) {
		var problems diag.Diagnostics
		problems.AddError("Timeout Cannot Be Parsed", "invalid")
		return 0, problems
	}, &invalid)
	done()
	assert.True(t, invalid.HasError(), "an unparsable timeout is reported")
}

// withTimeouts returns state with the timeouts block set to values; operations left out stay null.
func withTimeouts(t *testing.T, state tfsdk.State, values map[string]string) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	blockType, diagnostics := state.Schema.TypeAtPath(ctx, path.Root(timeoutsBlockName))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	attributeTypes := blockType.(timeouts.Type).AttrTypes
	attributes := map[string]attr.Value{}
	for name := range attributeTypes {
		attributes[name] = types.StringNull()
		if value, set := values[name]; set {
			attributes[name] = types.StringValue(value)
		}
	}
	diagnostics = state.SetAttribute(ctx, path.Root(timeoutsBlockName), timeouts.Value{Object: types.ObjectValueMust(attributeTypes, attributes)})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	return state
}

// deadlineClient records the deadline each statement's context carries.
type deadlineClient struct {
	// client answers the statements.
	client dataapi.Client
	// mu guards deadlines.
	mu sync.Mutex
	// deadlines holds one entry per statement; the zero time means no deadline.
	deadlines []time.Time
	// statements holds the SQL in the order of deadlines.
	statements []string
}

// Query records the context deadline and forwards the statement.
func (d *deadlineClient) Query(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
	deadline, _ := ctx.Deadline()
	d.mu.Lock()
	d.deadlines = append(d.deadlines, deadline)
	d.statements = append(d.statements, sql)
	d.mu.Unlock()
	return d.client.Query(ctx, target, sql, parameters)
}

// runTimed runs one lifecycle operation of a case with the given timeouts in its plan and state.
func runTimed(t *testing.T, test lifecycleCase, operation string, values map[string]string) (*deadlineClient, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	c := test.catalog()
	switch {
	case operation == "create" && test.absent != nil:
		test.absent(c)
	case operation == "delete":
		test.removable(c)
	}
	test.applyPrepare(c, operation)
	client := &deadlineClient{client: c}
	r := test.new()
	configureTestResource(t, r, client)
	state := withTimeouts(t, testState(t, r, test.model), values)
	plan := tfsdk.Plan(state)
	switch operation {
	case "create":
		resp := resource.CreateResponse{State: emptyState(t, r)}
		r.Create(ctx, resource.CreateRequest{Plan: plan, Config: tfsdk.Config(state)}, &resp)
		if !resp.Diagnostics.HasError() {
			var stored timeouts.Value
			require.False(t, resp.State.GetAttribute(ctx, path.Root(timeoutsBlockName), &stored).HasError())
			assert.Equal(t, values["create"], stored.Attributes()["create"].(types.String).ValueString(), "create keeps the planned timeouts in state")
		}
		return client, resp.Diagnostics
	case "update":
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state, Config: tfsdk.Config(state)}, &resp)
		return client, resp.Diagnostics
	default:
		resp := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
		return client, resp.Diagnostics
	}
}

// TestOperationTimeoutsBoundEveryStatement runs create, update, and delete of every lifecycle case with a timeouts
// block. With a timeout, each statement's context ends with the operation; without one, statements carry no deadline
// of the resource's own, except the bounded wait for an inbound datashare.
func TestOperationTimeoutsBoundEveryStatement(t *testing.T) {
	configured := map[string]string{"create": "1h", "update": "2h", "delete": "3h"}
	covered := map[string]bool{}
	for _, test := range lifecycleCases() {
		r := test.new()
		var response resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &response)
		if _, ok := response.Schema.Blocks[timeoutsBlockName]; !ok {
			continue
		}
		covered["redshift_"+resourceTypeName(test.new)] = true
		for _, operation := range []string{"create", "update", "delete"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				timeout, err := time.ParseDuration(configured[operation])
				require.NoError(t, err)
				started := time.Now()
				client, diagnostics := runTimed(t, test, operation, configured)
				require.False(t, diagnostics.HasError(), "%v", diagnostics)
				require.NotEmpty(t, client.deadlines)
				for i, deadline := range client.deadlines {
					assert.WithinRange(t, deadline, started.Add(timeout), time.Now().Add(timeout), "statement %d: %s", i, client.statements[i])
				}

				client, diagnostics = runTimed(t, test, operation, nil)
				require.False(t, diagnostics.HasError(), "%v", diagnostics)
				for i, deadline := range client.deadlines {
					if strings.Contains(client.statements[i], "svv_datashares") && test.name == "database" && operation == "create" {
						assert.WithinRange(t, deadline, started.Add(databaseShareWait), time.Now().Add(databaseShareWait), "the share wait keeps its own bound")
						continue
					}
					assert.True(t, deadline.IsZero(), "statement %d has a deadline without timeouts: %s", i, client.statements[i])
				}
			})
		}
	}
	assert.Equal(t, timedResources, slices.Sorted(maps.Keys(covered)), "every resource with timeouts needs a lifecycle case")
}

// TestOperationTimeoutCancelsStatement ends a statement that outlasts the create timeout and names the timeout.
func TestOperationTimeoutCancelsStatement(t *testing.T) {
	test := lifecycleRegistry.entries["table"]
	r := test.new()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, _ dataapi.Connection, _ string, _ map[string]string) ([]dataapi.Row, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	state := withTimeouts(t, testState(t, r, test.model), map[string]string{"create": "50ms"})
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state), Config: tfsdk.Config(state)}, &resp)
	require.Len(t, resp.Diagnostics.Errors(), 2, "%v", resp.Diagnostics)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), context.DeadlineExceeded.Error())
	assert.Equal(t, "Operation timed out", resp.Diagnostics.Errors()[1].Summary())
	assert.Contains(t, resp.Diagnostics.Errors()[1].Detail(), "create timeout of 50ms")
}

// TestDatabaseShareWaitFollowsCreateTimeout ends the wait for an inbound datashare when the create timeout expires,
// rather than after the default wait.
func TestDatabaseShareWaitFollowsCreateTimeout(t *testing.T) {
	test := lifecycleRegistry.entries["database"]
	r := test.new()
	configureTestResource(t, r, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return nil, nil
	}))
	state := withTimeouts(t, testState(t, r, test.model), map[string]string{"create": "100ms"})
	started := time.Now()
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state), Config: tfsdk.Config(state)}, &resp)
	assert.Less(t, time.Since(started), databaseShareWait)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "within 100ms")
}

// TestTableTimeoutsPlan configures, changes, and validates a timeouts block in real plans: changing it updates the
// table in place without DDL, and the lookup of the same table has no timeouts.
func TestTableTimeoutsPlan(t *testing.T) {
	c := fullCatalog()
	delete(fakeState[*tableFake](c, "table").tables, "serving.events")
	lookup := `
data "redshift_table" "events" {
  database = redshift_table.events.database
  schema   = redshift_table.events.schema
  name     = redshift_table.events.name
}
`
	config := func(timeouts string) string {
		return tableConfig(tableIDColumn+tableLabelColumn, `
  timeouts {`+timeouts+`
  }`) + lookup
	}
	var applied int
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: tablePlanProviders(c),
		Steps: []testresource.TestStep{
			{Config: config(`
    create = "10m"`), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.create", "10m"),
				testresource.TestCheckNoResourceAttr("redshift_table.events", "timeouts.update"),
				testresource.TestCheckNoResourceAttr("data.redshift_table.events", "timeouts.%"),
				testresource.TestCheckResourceAttr("data.redshift_table.events", "column.0.name", "id"),
			)},
			{Config: config(`
    create = "10m"
    update = "5m"
    delete = "2m"`), PreConfig: func() { applied = len(c.writes) }, ConfigPlanChecks: tableExpectUpdate(), Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.update", "5m"),
				testresource.TestCheckResourceAttr("redshift_table.events", "timeouts.delete", "2m"),
				func(*terraform.State) error {
					assert.Len(t, c.writes, applied, "changing timeouts runs no DDL")
					return nil
				},
			)},
			{Config: config(`
    create = "10m"
    update = "5m"
    delete = "2m"`), PlanOnly: true},
			{Config: config(`
    update = "0s"`), PlanOnly: true, ExpectError: regexp.MustCompile(`must be positive`)},
			{Config: config(`
    read = "1m"`), PlanOnly: true, ExpectError: regexp.MustCompile(`Unsupported argument`)},
			{Config: config(""), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate),
			}}, Check: testresource.TestCheckNoResourceAttr("redshift_table.events", "timeouts.create")},
		},
	})
}

// TestLocalDatabaseTimeoutsBoundEveryStatement covers the local create, with its option and collation reads, and the
// local update, the only database update that runs ALTER statements, which the shared lifecycle case cannot reach.
func TestLocalDatabaseTimeoutsBoundEveryStatement(t *testing.T) {
	configured := map[string]string{"create": "1h", "update": "2h"}
	for _, operation := range []string{"create", "update"} {
		for _, values := range []map[string]string{configured, nil} {
			t.Run(fmt.Sprintf("%s/%t", operation, values != nil), func(t *testing.T) {
				ctx := context.Background()
				present, isolation := operation == "update", "Snapshot Isolation"
				client := &deadlineClient{client: localDatabaseClient(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
					switch {
					case strings.HasPrefix(sql, "SHOW DATABASES"):
						if !present {
							return nil, nil
						}
						return []dataapi.Row{{"database_name": "warehouse", "database_type": "local", "database_isolation_level": isolation}}, nil
					case strings.HasPrefix(sql, `CREATE DATABASE "warehouse"`), strings.HasPrefix(sql, `ALTER DATABASE "warehouse"`):
						present = true
						if strings.Contains(sql, "SERIALIZABLE") {
							isolation = "Serializable"
						}
						return nil, nil
					}
					return nil, fmt.Errorf("unexpected SQL %q", sql)
				})}
				r := &databaseResource{testResourceClient(client)}
				model := databaseModel{Name: types.StringValue("warehouse"), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true), IsolationLevel: types.StringValue("SERIALIZABLE")}
				plan := tfsdk.Plan(withTimeouts(t, testState(t, r, model), values))
				started := time.Now()
				var diagnostics diag.Diagnostics
				if operation == "create" {
					resp := resource.CreateResponse{State: emptyState(t, r)}
					r.Create(ctx, resource.CreateRequest{Plan: plan, Config: tfsdk.Config(plan)}, &resp)
					diagnostics = resp.Diagnostics
				} else {
					model.IsolationLevel = types.StringValue("SNAPSHOT")
					state := withTimeouts(t, testState(t, r, model), values)
					resp := resource.UpdateResponse{State: state}
					r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state, Config: tfsdk.Config(plan)}, &resp)
					diagnostics = resp.Diagnostics
				}
				require.False(t, diagnostics.HasError(), "%v", diagnostics)
				assert.Equal(t, "Serializable", isolation, "the operation changed the database")
				for i, deadline := range client.deadlines {
					if values == nil {
						assert.True(t, deadline.IsZero(), "statement %d has a deadline without timeouts: %s", i, client.statements[i])
						continue
					}
					timeout, err := time.ParseDuration(values[operation])
					require.NoError(t, err)
					assert.WithinRange(t, deadline, started.Add(timeout), time.Now().Add(timeout), "statement %d: %s", i, client.statements[i])
				}
				if operation == "create" {
					assert.True(t, slices.ContainsFunc(client.statements, func(sql string) bool { return strings.HasPrefix(sql, "SELECT db_collation()") }),
						"create reads the collation inside the new database: %v", client.statements)
				} else {
					assert.Contains(t, client.statements, `ALTER DATABASE "warehouse" ISOLATION LEVEL SERIALIZABLE`)
				}
			})
		}
	}
}
