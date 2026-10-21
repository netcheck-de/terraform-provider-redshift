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
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "materialized view", new: newMaterializedViewResource, model: testMaterializedViewModel(),
	absent: func(c *catalog) { c.family("view").(*viewFamily).remove(fakeMaterializedViewName) },
})

var _ = registerReplacementPolicy("redshift_materialized_view", map[string]replaceRule{
	"database":     replaceAlways,
	"schema":       replaceAlways,
	"name":         replaceAlways,
	"query":        replaceAlways,
	"backup":       replaceAlways,
	"distribution": replaceNever,
	"sort_key":     replaceNever,
	"auto_refresh": replaceNever,
	"owner":        replaceNever,
})

var _ = registerValidateConfigCase("materialized view", validateConfigCase{
	new:   newMaterializedViewResource,
	valid: testMaterializedViewModel(),
	invalid: func() materializedViewModel {
		data := testMaterializedViewModel()
		data.Distribution = materializedViewDistributionBlock("ALL", "label")
		return data
	}(),
	unknown: func() materializedViewModel {
		data := testMaterializedViewModel()
		data.Distribution = types.ObjectValueMust(materializedViewDistributionTypes, map[string]attr.Value{"style": types.StringValue("KEY"), "key": types.StringUnknown()})
		return data
	}(),
})

// decodeMaterializedViewState reads a materialized view model from Terraform state.
func decodeMaterializedViewState(t *testing.T, state tfsdk.State) materializedViewModel {
	t.Helper()
	var data materializedViewResourceModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	return data.materializedViewModel
}

// TestMaterializedViewDefinitionDrift keeps the configured query across refreshes and surfaces an outside
// change, which plans a replacement because the query cannot be altered.
func TestMaterializedViewDefinitionDrift(t *testing.T) {
	c, views := viewFake()
	views.remove(fakeMaterializedViewName)
	r := newMaterializedViewResource()
	configureTestResource(t, r, c)
	planned := testMaterializedViewModel()
	planned.DefinitionFingerprint = types.StringUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	created := decodeMaterializedViewState(t, state)
	assert.Equal(t, planned.Query, created.Query)
	assert.True(t, created.AutoRefresh.ValueBool())
	assert.Equal(t, fakeViewOwner, created.Owner.ValueString())

	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, created, decodeMaterializedViewState(t, state))

	views.get(fakeMaterializedViewName).query = "SELECT label FROM serving.sales"
	views.get(fakeMaterializedViewName).autoRefresh = false
	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	drifted := decodeMaterializedViewState(t, state)
	assert.Equal(t, views.get(fakeMaterializedViewName).definition(), drifted.Query.ValueString())
	assert.False(t, drifted.AutoRefresh.ValueBool())
}

// TestMaterializedViewImportAdoptsConfiguration imports without a query and adopts the configured query and
// backup on the first update without SQL, so an import never plans a replacement by itself. A configured sort key
// is applied in place, and a warning shows the catalog definition that could not be compared.
func TestMaterializedViewImportAdoptsConfiguration(t *testing.T) {
	c, views := viewFake()
	r := newMaterializedViewResource()
	configureTestResource(t, r, c)
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	id := `{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"sales_summary"}`
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	read := resource.ReadResponse{State: imported.State}
	r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	data := decodeMaterializedViewState(t, read.State)
	assert.True(t, data.Query.IsNull(), "an import keeps the query unset")
	assert.True(t, data.AutoRefresh.ValueBool())
	assert.Equal(t, fakeViewOwner, data.Owner.ValueString())
	assert.Equal(t, definitionFingerprint(views.get(fakeMaterializedViewName).definition()), data.DefinitionFingerprint.ValueString())

	planned := testMaterializedViewModel()
	planned.ID, planned.Backup, planned.SortKey = data.ID, types.BoolValue(false), materializedViewSortKeyBlock("label")
	state, diagnostics := applyOperation(t, r, "update", data, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	adopted := decodeMaterializedViewState(t, state)
	assert.Equal(t, planned.Query, adopted.Query)
	assert.Equal(t, planned.SortKey, adopted.SortKey)
	assert.Equal(t, []string{`ALTER MATERIALIZED VIEW "serving"."sales_summary" ALTER COMPOUND SORTKEY ("label")`}, c.writes, "only the sort key runs SQL")
	require.Len(t, diagnostics.Warnings(), 1)
	assert.Contains(t, diagnostics.Warnings()[0].Detail(), views.get(fakeMaterializedViewName).definition())

	_, diagnostics = applyOperation(t, r, "update", adopted, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, diagnostics.Warnings(), "only the adopting update warns")
}

// materializedViewTestPrivate is private state holding fixed keys.
type materializedViewTestPrivate map[string][]byte

// GetKey returns the stored value of key.
func (p materializedViewTestPrivate) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return p[key], nil
}

// TestMaterializedViewReplaces adopts a null creation-only option only right after an import; a null option on
// a view Terraform created was omitted from CREATE, so setting it later replaces the view.
func TestMaterializedViewReplaces(t *testing.T) {
	ctx := context.Background()
	imported := materializedViewTestPrivate{materializedViewImportedKey: []byte("true")}
	assert.False(t, materializedViewReplaces(ctx, types.StringNull(), imported))
	assert.True(t, materializedViewReplaces(ctx, types.StringValue("EVEN"), imported))
	assert.True(t, materializedViewReplaces(ctx, types.BoolNull(), materializedViewTestPrivate{}))
	assert.True(t, materializedViewReplaces(ctx, types.ListNull(types.StringType), nil))
}

// materializedViewHiddenRefresh answers SVV_MV_INFO with no rows, as Redshift does for a regular user that
// does not own the view, and passes everything else to the fake catalog.
func materializedViewHiddenRefresh(c *catalog) queryFunc {
	return func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_mv_info") {
			return nil, nil
		}
		return c.Query(ctx, target, sql, parameters)
	}
}

// TestMaterializedViewRejectsOrdinaryViewAndBadRefreshRows refuses an ordinary view and SVV_MV_INFO rows that
// are malformed or ambiguous.
func TestMaterializedViewRejectsOrdinaryViewAndBadRefreshRows(t *testing.T) {
	c, _ := viewFake()
	r := &materializedViewResource{testResourceClient(c)}
	ordinary := testMaterializedViewModel()
	ordinary.Name = types.StringValue(fakeViewName)
	_, _, err := r.read(context.Background(), ordinary)
	require.ErrorContains(t, err, "redshift_view")
	ambiguous := &materializedViewResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_mv_info") {
			return []sqlclient.Row{{"autorefresh": "t"}, {"autorefresh": "f"}}, nil
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	_, _, err = ambiguous.read(context.Background(), testMaterializedViewModel())
	require.ErrorContains(t, err, "ambiguous")
	malformed := &materializedViewResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_mv_info") {
			return []sqlclient.Row{{"autorefresh": "u"}}, nil
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	_, _, err = malformed.read(context.Background(), testMaterializedViewModel())
	require.ErrorContains(t, err, "flag")
	assert.Empty(t, c.writes)
}

// TestMaterializedViewHiddenRefreshSetting keeps working when SVV_MV_INFO hides the view after an ownership
// transfer: pg_views decides existence, auto_refresh keeps its known value, and a warning explains why.
func TestMaterializedViewHiddenRefreshSetting(t *testing.T) {
	c, views := viewFake()
	views.remove(fakeMaterializedViewName)
	r := newMaterializedViewResource()
	configureTestResource(t, r, materializedViewHiddenRefresh(c))
	planned := testMaterializedViewModel()
	planned.Owner, planned.DefinitionFingerprint = types.StringValue("reporter"), types.StringUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, diagnostics.Warnings(), 1)
	assert.Contains(t, diagnostics.Warnings()[0].Detail(), `owned by "reporter"`)
	created := decodeMaterializedViewState(t, state)
	assert.True(t, created.AutoRefresh.ValueBool(), "the planned setting is kept")
	assert.Equal(t, "reporter", created.Owner.ValueString())

	views.get(fakeMaterializedViewName).autoRefresh = false
	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Len(t, diagnostics.Warnings(), 1)
	assert.Equal(t, created, decodeMaterializedViewState(t, state), "a hidden setting cannot drift")

	_, diagnostics = applyOperation(t, r, "delete", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Nil(t, views.get(fakeMaterializedViewName))
}

// TestMaterializedViewConvergenceFailures reports a refresh setting or owner the catalog does not reflect.
func TestMaterializedViewConvergenceFailures(t *testing.T) {
	for name, ignore := range map[string]string{"auto refresh": "ALTER MATERIALIZED VIEW", "owner": "ALTER TABLE"} {
		t.Run(name, func(t *testing.T) {
			c, _ := viewFake()
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, ignore) {
					return nil, nil // An acknowledged statement without effect.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newMaterializedViewResource()
			configureTestResource(t, r, client)
			planned := testMaterializedViewModel()
			if name == "owner" {
				planned.Owner = types.StringValue("reporter")
			} else {
				planned.AutoRefresh = types.BoolValue(false)
			}
			_, diagnostics := applyOperation(t, r, "update", testMaterializedViewModel(), planned, nil)
			require.True(t, diagnostics.HasError())
		})
	}
}

// TestMaterializedViewRejectsInvalidConfigurationBeforeSQL validates storage options and names before any SQL
// or state write.
func TestMaterializedViewRejectsInvalidConfigurationBeforeSQL(t *testing.T) {
	r := newMaterializedViewResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("unexpected SQL " + sql)
	}))
	invalid := testMaterializedViewModel()
	invalid.Distribution = materializedViewDistributionBlock("KEY", "")
	state, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull(), "a rejected view must not be recorded in state")
	unnamed := testMaterializedViewModel()
	unnamed.Schema = types.StringValue("")
	_, diagnostics = applyOperation(t, r, "update", testMaterializedViewModel(), unnamed, nil)
	require.True(t, diagnostics.HasError())
}

// TestMaterializedViewUpdateFailures reports a refused ALTER and a view dropped after planning.
func TestMaterializedViewUpdateFailures(t *testing.T) {
	c, views := viewFake()
	refusal := errors.New("AUTO REFRESH is not supported for materialized views with mutable functions")
	r := newMaterializedViewResource()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "ALTER MATERIALIZED VIEW") {
			return nil, refusal
		}
		return c.Query(ctx, target, sql, parameters)
	}))
	planned := testMaterializedViewModel()
	planned.AutoRefresh = types.BoolValue(false)
	_, diagnostics := applyOperation(t, r, "update", testMaterializedViewModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), refusal.Error())
	assert.True(t, views.get(fakeMaterializedViewName).autoRefresh)
	views.remove(fakeMaterializedViewName)
	_, diagnostics = applyOperation(t, r, "update", testMaterializedViewModel(), planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "disappeared")
	assert.Empty(t, c.writes)
}

// TestMaterializedViewTranscripts records refresh and ownership changes beyond the lifecycle case.
func TestMaterializedViewTranscripts(t *testing.T) {
	with := func(change func(*materializedViewModel)) materializedViewModel {
		data := testMaterializedViewModel()
		change(&data)
		return data
	}
	runTranscripts(t, "materialized_view_transcript", newMaterializedViewResource, []transcriptCase{
		{name: "create_all_options", operation: "create", catalog: catalogWith(func(c *catalog) { c.family("view").(*viewFamily).remove(fakeMaterializedViewName) }), planned: with(func(data *materializedViewModel) {
			data.Backup, data.Distribution = types.BoolValue(false), materializedViewDistributionBlock("KEY", "label")
			data.SortKey, data.AutoRefresh, data.Owner = materializedViewSortKeyBlock("label"), types.BoolValue(false), types.StringUnknown()
		})},
		{name: "update_auto_refresh", operation: "update", catalog: catalogWith(), prior: testMaterializedViewModel(), planned: with(func(data *materializedViewModel) {
			data.AutoRefresh = types.BoolValue(false)
		})},
		{name: "update_owner", operation: "update", catalog: catalogWith(), prior: testMaterializedViewModel(), planned: with(func(data *materializedViewModel) {
			data.Owner = types.StringValue("reporter")
		})},
		{name: "update_storage", operation: "update", catalog: catalogWith(), prior: with(func(data *materializedViewModel) {
			data.Distribution, data.SortKey = materializedViewDistributionBlock("ALL", ""), materializedViewSortKeyBlock("label")
		}), planned: with(func(data *materializedViewModel) {
			data.Distribution, data.SortKey, data.Owner = materializedViewDistributionBlock("", "label"), materializedViewSortKeyBlock("label", "sales"), types.StringValue("reporter")
		})},
	})
}
