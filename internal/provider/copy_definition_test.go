package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyJobDefinitionDrift(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*copyJobFake](c, copyJobFakeFamily)
	fake.definition = `COPY "other schema"."other table" ("label", "id") FROM 's3://other/data/' IAM_ROLE '' FORMAT JSON 'auto ignorecase' IGNOREHEADER 2 JOB CREATE events_load AUTO OFF;`
	r := newCopyJobResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "read", copyJobLifecycleModel, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed copyJobModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, "other schema", copyJobTarget(observed.Table).schema)
	assert.Equal(t, "other table", copyJobTarget(observed.Table).name)
	assert.Equal(t, []string{"label", "id"}, copyJobTarget(observed.Table).columns)
	assert.Equal(t, "s3://other/data/", observed.Source.ValueString())
	assert.Equal(t, "JSON", observed.Format.ValueString())
	assert.Equal(t, "auto ignorecase", observed.FormatArgument.ValueString())
	assert.Equal(t, map[string]string{"IGNOREHEADER": "2"}, knownMap(observed.Parameters))
	assert.Equal(t, copyJobFakeRole, observed.IAMRole.ValueString(), "redacted SQL does not erase IAM role")

	for _, definition := range []string{"", `COPY serving.events FROM 's3://bucket/' IAM_ROLE '' XML JOB CREATE events_load`, `COPY serving.events FROM 's3://bucket/' IAM_ROLE '' JOB CREATE wrong`, `COPY serving.events FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load; DROP TABLE events`, `COPY events FROM 's3://bucket/' IAM_ROLE '' JOB CREATE events_load`} {
		t.Run(definition, func(t *testing.T) {
			fake.definition = definition
			_, diagnostics := applyOperation(t, r, "read", copyJobLifecycleModel, nil, nil)
			assert.True(t, diagnostics.HasError(), "unsupported definitions must not silently keep stale state")
		})
	}
}

func TestCopyJobDefinitionConvergence(t *testing.T) {
	c := fullCatalog()
	fakeState[*copyJobFake](c, copyJobFakeFamily).exists = false
	r := newCopyJobResource()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, connection, sql, parameters)
		if strings.HasPrefix(sql, "COPY ") {
			fake := fakeState[*copyJobFake](c, copyJobFakeFamily)
			fake.definition = strings.ReplaceAll(fake.definition, "s3://example-bucket/events/", "s3://wrong-bucket/events/")
		}
		return rows, err
	}))
	state, diagnostics := applyOperation(t, r, "create", nil, copyJobLifecycleModel, nil)
	assert.True(t, diagnostics.HasError())
	assert.False(t, state.Raw.IsNull(), "a created job must remain tracked when definition verification fails")
}

func TestCopyPartialKnownValidation(t *testing.T) {
	ctx := context.Background()
	for _, job := range []bool{false, true} {
		t.Run(map[bool]string{false: "template", true: "job"}[job], func(t *testing.T) {
			validate := func(parameters types.Map, format, argument types.String) bool {
				var r resource.Resource
				var model any
				if job {
					r = newCopyJobResource()
					data := copyJobLifecycleModel
					data.Source = types.StringUnknown()
					data.Parameters, data.Format, data.FormatArgument = parameters, format, argument
					model = data
				} else {
					r = newTemplateResource()
					data := templateLifecycleModel
					data.Database = types.StringUnknown()
					data.Parameters, data.Format, data.FormatArgument = parameters, format, argument
					model = data
				}
				var resp resource.ValidateConfigResponse
				r.(resource.ResourceWithValidateConfig).ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &resp)
				return resp.Diagnostics.HasError()
			}
			partial := types.MapValueMust(types.StringType, map[string]attr.Value{"IGNOREHEADER": types.StringValue("bad"), "DELIMITER": types.StringUnknown()})
			assert.True(t, validate(partial, types.StringUnknown(), types.StringUnknown()), "known invalid values cannot hide behind unrelated unknowns")
			partial = types.MapValueMust(types.StringType, map[string]attr.Value{"MANIFEST": types.StringUnknown()})
			assert.True(t, validate(partial, types.StringUnknown(), types.StringUnknown()), "known unsupported keys fail even with unknown values")
			partial = types.MapValueMust(types.StringType, map[string]attr.Value{"DELIMITER": types.StringUnknown()})
			assert.False(t, validate(partial, types.StringValue("JSON"), types.StringUnknown()))
			assert.True(t, validate(partial, types.StringValue("CSV"), types.StringValue("auto")))
		})
	}
}

func TestCopyRenderersRejectUnknownDefinitions(t *testing.T) {
	template := templateLifecycleModel
	template.Parameters = types.MapUnknown(types.StringType)
	_, err := createTemplateStatements(template)
	require.Error(t, err)
	job := copyJobLifecycleModel
	job.Source = types.StringUnknown()
	_, err = createCopyJobStatement(job)
	require.Error(t, err)
	job = copyJobLifecycleModel
	target := job.Table.Attributes()
	target["columns"] = types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})
	job.Table = types.ObjectValueMust(copyJobTableType.AttrTypes, target)
	_, err = createCopyJobStatement(job)
	require.Error(t, err)
}

func TestCopyShapefileSimplify(t *testing.T) {
	for _, argument := range []string{"SIMPLIFY", "SIMPLIFY AUTO", "SIMPLIFY 0.01", "SIMPLIFY AUTO 0.000001", "simplify auto 1e-06"} {
		t.Run(argument, func(t *testing.T) {
			model := templateTestModel("spatial", "SHAPEFILE", argument, nil)
			statements, err := createTemplateStatements(model)
			require.NoError(t, err)
			definition, err := parseTemplateDefinition(statements[0])
			require.NoError(t, err)
			entry := templateCatalog{definition: definition}
			entry.applyCatalog(&model)
			assert.Equal(t, argument, model.FormatArgument.ValueString())
		})
	}
	for _, argument := range []string{"AUTO", "SIMPLIFY -1", "SIMPLIFY NaN", "SIMPLIFY Inf", "SIMPLIFY 1; DROP", "SIMPLIFY AUTO 1 2"} {
		_, err := copyCheckFormat("SHAPEFILE", argument)
		require.Error(t, err, argument)
	}
	definition, err := parseTemplateDefinition(`CREATE TEMPLATE spatial FOR COPY AS SHAPEFILE SIMPLIFY AUTO 0.000001;`)
	require.NoError(t, err)
	assert.Equal(t, "SIMPLIFY AUTO 0.000001", definition.formatArgument)
	definition, err = parseTemplateDefinition(`CREATE OR REPLACE TEMPLATE dev.public.spatial FOR COPY AS SIMPLIFY AUTO 0.000001 FORMAT AS SHAPEFILE;`)
	require.NoError(t, err)
	assert.Equal(t, "SIMPLIFY AUTO 0.000001", definition.formatArgument, "live SHOW prints SIMPLIFY before the format")
}
