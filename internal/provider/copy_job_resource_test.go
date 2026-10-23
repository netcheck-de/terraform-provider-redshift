package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "copy job", new: newCopyJobResource, model: copyJobLifecycleModel,
	absent:     func(c *catalog) { fakeState[*copyJobFake](c, copyJobFakeFamily).exists = false },
	isDeletion: func(sql string) bool { return strings.HasPrefix(sql, "COPY JOB DROP ") },
})

var _ = registerReplacementPolicy("redshift_copy_job", map[string]replaceRule{
	"database":        replaceAlways,
	"name":            replaceAlways,
	"iam_role":        replaceAlways,
	"source":          replaceAlways,
	"format":          replaceConditional,
	"format_argument": replaceAlways,
	"parameters":      replaceAlways,
	"table":           replaceAlways,
	"auto":            replaceNever,
})

var _ = registerValidateConfigCase("copy_job", validateConfigCase{
	new:     newCopyJobResource,
	valid:   copyJobLifecycleModel,
	invalid: copyJobTestModel("events_load", map[string]string{"MAXERROR": "10"}),
	unknown: func() copyJobModel {
		model := copyJobTestModel("events_load", nil)
		model.Format, model.Parameters = types.StringNull(), types.MapUnknown(types.StringType)
		return model
	}(),
})

// TestCopyJobTranscripts records AUTO changes, the role drift read, a cut role, and import.
func TestCopyJobTranscripts(t *testing.T) {
	automatic := copyJobLifecycleModel
	automatic.Auto = types.BoolValue(true)
	absent := func(c *catalog) { fakeState[*copyJobFake](c, copyJobFakeFamily).exists = false }
	runTranscripts(t, "copy_job/flows", newCopyJobResource, []transcriptCase{
		{name: "update_auto", operation: "update", catalog: catalogWith(), prior: copyJobLifecycleModel, planned: automatic},
		{name: "create_auto", operation: "create", catalog: catalogWith(absent), planned: automatic},
		{name: "import_without_configuration", operation: "import", catalog: catalogWith(absent), planned: copyJobLifecycleModel},
		{name: "delete_missing", operation: "delete", catalog: catalogWith(absent), prior: copyJobLifecycleModel},
	})
}

// TestCopyJobReadReconciles refreshes AUTO and the role, keeps the unreadable definition, and removes a dropped job.
func TestCopyJobReadReconciles(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*copyJobFake](c, copyJobFakeFamily)
	fake.auto, fake.role = true, "arn:aws:iam::123456789012:role/other"
	r := newCopyJobResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "read", copyJobLifecycleModel, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed copyJobModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.True(t, observed.Auto.ValueBool())
	assert.Equal(t, "arn:aws:iam::123456789012:role/other", observed.IAMRole.ValueString(), "another role surfaces to plan a replacement")
	assert.True(t, observed.Table.Equal(copyJobLifecycleModel.Table), "the definition is kept")
	assert.True(t, observed.Parameters.Equal(copyJobLifecycleModel.Parameters))

	fake.exists = false
	state, _ = applyOperation(t, r, "read", copyJobLifecycleModel, nil, nil)
	assert.True(t, state.Raw.IsNull(), "a dropped job is removed from state")

	ambiguous := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		row := sqlclient.Row{"job_name": "events_load", "iam_role": copyJobFakeRole, "is_auto": "1"}
		if strings.Contains(sql, "sys_copy_job") {
			return []sqlclient.Row{row, row}, nil
		}
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	})
	configureTestResource(t, r, ambiguous)
	assert.True(t, invoke(t, r, "read", copyJobLifecycleModel).HasError())
	configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"job_name": "events_load", "iam_role": copyJobFakeRole, "is_auto": "maybe"}}, nil
	}))
	assert.True(t, invoke(t, r, "read", copyJobLifecycleModel).HasError())
}

// TestCopyJobConvergence fails an apply whose AUTO setting or role did not take effect.
func TestCopyJobConvergence(t *testing.T) {
	c := fullCatalog()
	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "COPY JOB ALTER") {
			return nil, nil
		}
		return c.Query(ctx, connection, sql, parameters)
	})
	r := newCopyJobResource()
	configureTestResource(t, r, client)
	automatic := copyJobLifecycleModel
	automatic.Auto = types.BoolValue(true)
	_, diagnostics := applyOperation(t, r, "update", copyJobLifecycleModel, automatic, nil)
	assert.True(t, diagnostics.HasError())

	absent := fullCatalog()
	fakeState[*copyJobFake](absent, copyJobFakeFamily).exists = false
	created := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := absent.Query(ctx, connection, sql, parameters)
		if strings.HasPrefix(sql, "COPY ") {
			fakeState[*copyJobFake](absent, copyJobFakeFamily).role = "arn:aws:iam::123456789012:role/other"
		}
		return rows, err
	})
	configureTestResource(t, r, created)
	state, diagnostics := applyOperation(t, r, "create", nil, copyJobLifecycleModel, nil)
	assert.True(t, diagnostics.HasError())
	assert.False(t, state.Raw.IsNull(), "a created job stays in state when verification fails")
}

// TestCopyJobCreateRejectsInvalidBeforeState keeps invalid jobs out of state and SQL.
func TestCopyJobCreateRejectsInvalidBeforeState(t *testing.T) {
	r := newCopyJobResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))
	model := copyJobTestModel("events_load", map[string]string{"IGNOREALLERRORS": ""})
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, model))}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "an invalid job must not be recorded in state")

	model = copyJobLifecycleModel
	model.Table = types.ObjectNull(copyJobTableType.AttrTypes)
	var validated resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &validated)
	assert.False(t, validated.Diagnostics.HasError(), "the schema reports an absent table block once: %v", validated.Diagnostics)
}

// TestCopyJobImportAdoptsDefinition compares imports to the actual definition, never adopting mismatched configuration.
func TestCopyJobImportAdoptsDefinition(t *testing.T) {
	ctx := context.Background()
	r := newCopyJobResource()
	configureTestResource(t, r, fullCatalog())
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	id := r.(*copyJobResource).identity("admin", map[string]string{"name": "events_load"}).ValueString()
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	refreshed := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &refreshed)
	require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)
	var state copyJobModel
	require.False(t, refreshed.State.Get(ctx, &state).HasError())
	assert.Equal(t, copyJobFakeRole, state.IAMRole.ValueString())
	assert.Equal(t, copyJobLifecycleModel.Source, state.Source, "imports read the actual definition")
	assert.Equal(t, copyJobLifecycleModel.Table, state.Table)

	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)
	plan := tfsdk.Plan(testState(t, r, copyJobLifecycleModel))
	managed := testState(t, r, func() copyJobModel {
		model := copyJobLifecycleModel
		model.Source = types.StringValue("s3://example-bucket/old/")
		model.Format = types.StringValue("JSON")
		model.FormatArgument = types.StringValue("auto")
		model.Table = copyJobTableObject("serving", "old_events")
		model.Parameters = templateTestModel("", "", "", map[string]string{"GZIP": ""}).Parameters
		return model
	}())
	for name, prior := range map[string]tfsdk.State{"imported": refreshed.State, "managed": managed} {
		for _, attribute := range []string{"source", "format", "format_argument", "parameters", "table"} {
			replace := false
			var priorValue, planValue any
			switch attribute {
			case "parameters":
				var before, after types.Map
				require.False(t, prior.GetAttribute(ctx, path.Root(attribute), &before).HasError())
				require.False(t, plan.GetAttribute(ctx, path.Root(attribute), &after).HasError())
				for _, modifier := range response.Schema.Attributes[attribute].(schema.MapAttribute).PlanModifiers {
					resp := planmodifier.MapResponse{PlanValue: after}
					modifier.PlanModifyMap(ctx, planmodifier.MapRequest{Path: path.Root(attribute), State: prior, Plan: plan, StateValue: before, PlanValue: after, ConfigValue: after}, &resp)
					replace = replace || resp.RequiresReplace
				}
				priorValue, planValue = before, after
			case "table":
				var before, after types.Object
				require.False(t, prior.GetAttribute(ctx, path.Root(attribute), &before).HasError())
				require.False(t, plan.GetAttribute(ctx, path.Root(attribute), &after).HasError())
				for _, modifier := range response.Schema.Blocks[attribute].(schema.SingleNestedBlock).PlanModifiers {
					resp := planmodifier.ObjectResponse{PlanValue: after}
					modifier.PlanModifyObject(ctx, planmodifier.ObjectRequest{Path: path.Root(attribute), State: prior, Plan: plan, StateValue: before, PlanValue: after, ConfigValue: after}, &resp)
					replace = replace || resp.RequiresReplace
				}
				priorValue, planValue = before, after
			default:
				var before, after types.String
				require.False(t, prior.GetAttribute(ctx, path.Root(attribute), &before).HasError())
				require.False(t, plan.GetAttribute(ctx, path.Root(attribute), &after).HasError())
				for _, modifier := range response.Schema.Attributes[attribute].(schema.StringAttribute).PlanModifiers {
					resp := planmodifier.StringResponse{PlanValue: after}
					modifier.PlanModifyString(ctx, planmodifier.StringRequest{Path: path.Root(attribute), State: prior, Plan: plan, StateValue: before, PlanValue: after, ConfigValue: after}, &resp)
					replace = replace || resp.RequiresReplace
				}
				priorValue, planValue = before, after
			}
			assert.Equal(t, name == "managed", replace, "%s %s: %v -> %v", name, attribute, priorValue, planValue)
		}
	}
	// The format keyword compares without case, so another spelling records in place.
	var format types.String
	require.False(t, managed.GetAttribute(ctx, path.Root("format"), &format).HasError())
	lower := types.StringValue("json")
	for _, modifier := range response.Schema.Attributes["format"].(schema.StringAttribute).PlanModifiers {
		resp := planmodifier.StringResponse{PlanValue: lower}
		modifier.PlanModifyString(ctx, planmodifier.StringRequest{Path: path.Root("format"), State: managed, Plan: plan, StateValue: format, PlanValue: lower, ConfigValue: lower}, &resp)
		assert.False(t, resp.RequiresReplace)
	}
	// Matching imported definitions require no statement.
	statements, err := alterCopyJobStatements(state, copyJobLifecycleModel)
	require.NoError(t, err)
	assert.Empty(t, statements)
}

// TestCopyJobDefinitionRejectsMalformedText rejects stored COPY JOB text it cannot parse and requires a known target table.
func TestCopyJobDefinitionRejectsMalformedText(t *testing.T) {
	for _, text := range []string{`COPY 'unterminated`, `SELECT a.b`, `COPY`, `COPY a.`, `COPY wrong.a.b FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load`, `COPY a.b () FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load`, `COPY a.b (c FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load`, `COPY a.b`, `COPY a.b FROM 's3://bucket/'`} {
		t.Run(text, func(t *testing.T) {
			data := copyJobLifecycleModel
			require.Error(t, readCopyJobDefinition(&data, text))
		})
	}
	data := copyJobLifecycleModel
	require.NoError(t, readCopyJobDefinition(&data, `COPY admin.serving.events FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load`))
	assert.Equal(t, "serving", copyJobTarget(data.Table).schema)
	assert.Empty(t, copyJobTarget(types.ObjectNull(copyJobTableType.AttrTypes)))
	data = copyJobLifecycleModel
	attributes := data.Table.Attributes()
	attributes["schema"] = types.StringValue("")
	data.Table = types.ObjectValueMust(copyJobTableType.AttrTypes, attributes)
	require.Error(t, copyJobValidate(data))
	attributes["schema"] = types.StringUnknown()
	data.Table = types.ObjectValueMust(copyJobTableType.AttrTypes, attributes)
	_, err := createCopyJobStatement(data)
	require.ErrorContains(t, err, "table must be known")
}

// TestCopyJobLifecycleEdges treats a missing database as absence, records the server's auto value, and reports a failed ALTER.
func TestCopyJobLifecycleEdges(t *testing.T) {
	ctx := context.Background()
	c := fullCatalog()
	r := &copyJobResource{testResourceClient(c)}
	data := copyJobLifecycleModel
	data.Database = types.StringValue("missing")
	found, err := readCopyJob(ctx, &r.resourceClient, &data)
	require.NoError(t, err)
	assert.False(t, found)
	fakeState[*copyJobFake](c, copyJobFakeFamily).exists = false
	data = copyJobLifecycleModel
	data.Auto = types.BoolUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed copyJobModel
	require.False(t, state.Get(ctx, &observed).HasError())
	assert.True(t, observed.Auto.ValueBool())
	r = &copyJobResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "COPY JOB ALTER") {
			return nil, errors.New("alter denied")
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	planned := copyJobLifecycleModel
	planned.Auto = types.BoolValue(true)
	_, diagnostics = applyOperation(t, r, "update", copyJobLifecycleModel, planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "alter denied")
}
