package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "view", new: newViewResource, model: testViewModel(),
	absent: func(c *catalog) { c.family("view").(*viewFamily).remove(fakeViewName) },
})

var _ = registerReplacementPolicy("redshift_view", map[string]replaceRule{
	"database":     replaceAlways,
	"schema":       replaceAlways,
	"name":         replaceAlways,
	"query":        replaceNever,
	"late_binding": replaceNever,
	"owner":        replaceNever,
})

var _ = registerValidateConfigCase("view", validateConfigCase{
	new:   newViewResource,
	valid: testViewModel(),
	invalid: func() viewModel {
		data := testViewModel()
		data.Query = types.StringValue("SELECT 1; DROP TABLE serving.sales")
		return data
	}(),
	unknown: func() viewModel {
		data := testViewModel()
		data.Query = types.StringUnknown()
		return data
	}(),
})

// viewFake returns a full fake catalog and its view family.
func viewFake() (*catalog, *viewFamily) {
	c := fullCatalog()
	return c, fakeState[*viewFamily](c, "view")
}

// decodeViewState reads a view model from Terraform state.
func decodeViewState(t *testing.T, state tfsdk.State) viewModel {
	t.Helper()
	var data viewModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	return data
}

// TestViewDefinitionDrift keeps the configured query across refreshes, surfaces an outside change, and restores
// the configured query with CREATE OR REPLACE VIEW.
func TestViewDefinitionDrift(t *testing.T) {
	c, views := viewFake()
	views.remove(fakeViewName)
	r := newViewResource()
	configureTestResource(t, r, c)
	planned := testViewModel()
	planned.Query = types.StringValue("select id,\n  label from serving.sales")
	planned.DefinitionFingerprint = types.StringUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	created := decodeViewState(t, state)
	assert.Equal(t, planned.Query, created.Query, "state keeps the configured text")
	assert.Equal(t, definitionFingerprint(views.get(fakeViewName).definition()), created.DefinitionFingerprint.ValueString())
	assert.Equal(t, fakeViewOwner, created.Owner.ValueString())
	assert.Equal(t, fakeViewOwner, views.get(fakeViewName).owner)

	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, created, decodeViewState(t, state), "a refresh without outside changes is stable")

	views.get(fakeViewName).query = "SELECT id, 'outside' AS label FROM serving.sales"
	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	drifted := decodeViewState(t, state)
	assert.Equal(t, views.get(fakeViewName).definition(), drifted.Query.ValueString(), "an outside change surfaces the catalog text")

	c.writes = nil
	planned.ID = created.ID
	state, diagnostics = applyOperation(t, r, "update", drifted, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	restored := decodeViewState(t, state)
	assert.Equal(t, planned.Query, restored.Query)
	assert.Equal(t, []string{`CREATE OR REPLACE VIEW "serving"."sales_view" AS select id,` + "\n" + `  label from serving.sales`}, c.writes)
	assert.Equal(t, "select id,\n  label from serving.sales", views.get(fakeViewName).query)
}

// TestViewImportFillsCatalogDefinition imports the catalog definition, owner, and binding mode, so the first
// plan after import shows the configured query as an in-place change.
func TestViewImportFillsCatalogDefinition(t *testing.T) {
	c, views := viewFake()
	views.get(fakeViewName).lateBinding = true
	r := newViewResource()
	configureTestResource(t, r, c)
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	id := `{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"sales_view"}`
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	resp := resource.ReadResponse{State: imported.State}
	r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	data := decodeViewState(t, resp.State)
	assert.Equal(t, views.get(fakeViewName).definition(), data.Query.ValueString())
	assert.True(t, data.LateBinding.ValueBool())
	assert.Equal(t, fakeViewOwner, data.Owner.ValueString())
	assert.Equal(t, definitionFingerprint(data.Query.ValueString()), data.DefinitionFingerprint.ValueString())
	assert.Empty(t, c.writes, "import never mutates")
}

// TestViewImportPlansInPlaceReplace runs Terraform against the fake catalog: after an import, the catalog text
// differs from the configured query, so the plan updates the view in place, and the next plan is empty.
func TestViewImportPlansInPlaceReplace(t *testing.T) {
	c, views := viewFake()
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_view" "labels" {
  database = "admin"
  schema = %q
  name = %q
  query = %q
}`, fakeViewSchema, fakeViewName, "SELECT id,\n  label FROM serving.sales")
	address := "redshift_view.labels"
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{
				Config: configuration, ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":%q,"name":%q}`, fakeViewSchema, fakeViewName),
			},
			{
				Config:           configuration,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check: func(*terraform.State) error {
					c.mu.Lock()
					defer c.mu.Unlock()
					assert.Equal(t, "SELECT id,\n  label FROM serving.sales", views.get(fakeViewName).query)
					assert.Equal(t, fakeViewOwner, views.get(fakeViewName).owner, "CREATE OR REPLACE VIEW keeps the owner")
					return nil
				},
			},
			{Config: configuration, PlanOnly: true},
		},
	})
	assert.Nil(t, views.get(fakeViewName), "destroy drops the view")
}

// TestViewRejectsMaterializedRelation keeps the view resource and lookup away from materialized views.
func TestViewRejectsMaterializedRelation(t *testing.T) {
	c, _ := viewFake()
	r := &viewResource{testResourceClient(c)}
	data := testViewModel()
	data.Name = types.StringValue(fakeMaterializedViewName)
	_, _, err := r.read(context.Background(), data)
	require.ErrorIs(t, err, errViewIsMaterialized)
	for _, operation := range []string{"read", "update", "delete"} {
		configureTestResource(t, r, c)
		assert.True(t, invoke(t, r, operation, data, false).HasError(), operation)
	}
	assert.Empty(t, c.writes)
}

// TestViewReadRejectsIncompleteCatalogRows reports ambiguous or incomplete pg_views rows instead of guessing.
func TestViewReadRejectsIncompleteCatalogRows(t *testing.T) {
	for name, rows := range map[string][]sqlclient.Row{
		"no owner":      {{"viewowner": "", "definition": "SELECT 1"}},
		"no definition": {{"viewowner": "analyst", "definition": ""}},
		"two rows":      {{"viewowner": "analyst", "definition": "SELECT 1"}, {"viewowner": "analyst", "definition": "SELECT 1"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &viewResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return rows, nil
			}))}
			_, _, err := r.read(context.Background(), testViewModel())
			require.Error(t, err)
		})
	}
}

// TestViewCreateRejectsInvalidQueryBeforeSQL validates the query before any SQL or state write.
func TestViewCreateRejectsInvalidQueryBeforeSQL(t *testing.T) {
	r := newViewResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("unexpected SQL " + sql)
	}))
	data := testViewModel()
	data.Query = types.StringValue("SELECT 1; DROP VIEW serving.other")
	plan := testState(t, r, data)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "a rejected view must not be recorded in state")
	update := resource.UpdateResponse{State: plan}
	r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(plan), State: testState(t, r, testViewModel())}, &update)
	require.True(t, update.Diagnostics.HasError())
}

// TestViewConvergenceFailures reports an owner or binding mode that the catalog does not reflect after apply,
// while Create still records the created view.
func TestViewConvergenceFailures(t *testing.T) {
	for name, ignore := range map[string]string{"owner": "ALTER TABLE", "late binding": "CREATE OR REPLACE VIEW"} {
		t.Run(name, func(t *testing.T) {
			c, views := viewFake()
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, ignore) {
					return nil, nil // An acknowledged statement without effect.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newViewResource()
			configureTestResource(t, r, client)
			if name == "owner" {
				views.remove(fakeViewName)
				state, diagnostics := applyOperation(t, r, "create", nil, testViewModel(), nil)
				require.True(t, diagnostics.HasError())
				assert.Contains(t, diagnostics.Errors()[0].Detail(), `owned by "admin"`)
				assert.Equal(t, fakeViewName, decodeViewState(t, state).Name.ValueString(), "the created view stays in state")
				return
			}
			planned := testViewModel()
			planned.LateBinding = types.BoolValue(true)
			_, diagnostics := applyOperation(t, r, "update", testViewModel(), planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "late binding is false")
		})
	}
}

// TestViewUpdateRequiresExistingView refuses CREATE OR REPLACE for a view dropped after planning.
func TestViewUpdateRequiresExistingView(t *testing.T) {
	c, views := viewFake()
	views.remove(fakeViewName)
	r := newViewResource()
	configureTestResource(t, r, c)
	planned := testViewModel()
	planned.Query = types.StringValue("SELECT id FROM serving.sales")
	_, diagnostics := applyOperation(t, r, "update", testViewModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
	assert.Nil(t, views.get(fakeViewName))
}

// TestViewUpdateReportsRedshiftError surfaces a refused CREATE OR REPLACE VIEW, such as a changed column list,
// without dropping the view.
func TestViewUpdateReportsRedshiftError(t *testing.T) {
	c, views := viewFake()
	refusal := errors.New("cannot change number of columns in view")
	r := newViewResource()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "CREATE OR REPLACE VIEW") {
			return nil, refusal
		}
		return c.Query(ctx, target, sql, parameters)
	}))
	planned := testViewModel()
	planned.Query = types.StringValue("SELECT id FROM serving.sales")
	_, diagnostics := applyOperation(t, r, "update", testViewModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), refusal.Error())
	assert.Equal(t, fakeViewQuery, views.get(fakeViewName).query, "the view is left unchanged")
	assert.Empty(t, c.writes, "no DROP is attempted")
}

// TestViewReadRemovesViewOfMissingDatabase drops state when the view's database is gone, without reading pg_views
// in a database that no longer exists.
func TestViewReadRemovesViewOfMissingDatabase(t *testing.T) {
	c, _ := viewFake()
	r := newViewResource()
	configureTestResource(t, r, c)
	data := testViewModel()
	data.Database = types.StringValue("warehouse")
	state := testState(t, r, data)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.True(t, resp.State.Raw.IsNull())
}

// TestViewTranscripts records in-place definition, binding, and ownership changes beyond the lifecycle case.
func TestViewTranscripts(t *testing.T) {
	with := func(change func(*viewModel)) viewModel {
		data := testViewModel()
		change(&data)
		return data
	}
	late := with(func(data *viewModel) { data.LateBinding = types.BoolValue(true) })
	runTranscripts(t, "view_transcript", newViewResource, []transcriptCase{
		{name: "create_late_binding_unowned", operation: "create", catalog: catalogWith(func(c *catalog) { c.family("view").(*viewFamily).remove(fakeViewName) }), planned: with(func(data *viewModel) {
			data.LateBinding, data.Owner = types.BoolValue(true), types.StringUnknown()
		})},
		{name: "update_query", operation: "update", catalog: catalogWith(), prior: testViewModel(), planned: with(func(data *viewModel) {
			data.Query = types.StringValue("SELECT id, label, 1 AS version FROM serving.sales")
		})},
		{name: "update_late_binding", operation: "update", catalog: catalogWith(), prior: testViewModel(), planned: late},
		{name: "update_owner", operation: "update", catalog: catalogWith(), prior: testViewModel(), planned: with(func(data *viewModel) { data.Owner = types.StringValue("reporter") })},
		{name: "read_late_binding", operation: "read", catalog: catalogWith(func(c *catalog) { c.family("view").(*viewFamily).get(fakeViewName).lateBinding = true }), prior: late},
	})
}
