package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageCopyParameters(t *testing.T) {
	assert.Equal(t, "decoded", copyLiteralValue(sqlclient.Token{Text: "decoded"}))
	assert.Equal(t, "\b\f\v", copyLiteralValue(sqlclient.Token{Raw: `'\b\f\v'`}))
	for _, parameters := range []map[string]string{{"MANIFEST": ""}, {"IGNOREHEADER": "bad"}} {
		_, err := copyCanonicalParameters(parameters, false)
		require.Error(t, err)
		_, err = copyParameterFragments(parameters, false)
		require.Error(t, err)
	}
	for _, argument := range []string{"", "SIMPLIFY -0"} {
		fragment, err := copySimplify(argument)
		require.NoError(t, err)
		assert.NotContains(t, fragment.String(), "-0")
	}
	assert.Equal(t, types.StringValue(""), copyFormatArgumentValue(types.StringValue(""), "CSV", ""))
	fragment, err := copyParameterFragment("ACCEPTINVCHARS", "", false)
	require.NoError(t, err)
	assert.Equal(t, "ACCEPTINVCHARS", fragment.String())
	for _, text := range []string{"SHAPEFILE SIMPLIFY SIMPLIFY", "SHAPEFILE SIMPLIFY 'bad'", "CSV SIMPLIFY"} {
		tokens, err := sqlclient.Tokens(text)
		require.NoError(t, err)
		_, err = parseCopyOptions(tokens)
		require.Error(t, err)
	}
	require.Error(t, copyValidateKnown(types.StringValue("bad"), types.StringUnknown(), types.MapNull(types.StringType), false))
	nullParameter := types.MapValueMust(types.StringType, map[string]attr.Value{"DELIMITER": types.StringNull()})
	require.Error(t, copyValidateKnown(types.StringNull(), types.StringNull(), nullParameter, false))
	unknownParameter := types.MapValueMust(types.StringType, map[string]attr.Value{"DELIMITER": types.StringUnknown()})
	assert.False(t, copyOptionsKnown(types.StringNull(), types.StringNull(), unknownParameter))
}

func TestCoverageCopyJobDefinition(t *testing.T) {
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

func TestCoverageCopyJobLifecycle(t *testing.T) {
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

func TestCoverageCopyTemplate(t *testing.T) {
	ctx := context.Background()
	r := &templateResource{testResourceClient(fullCatalog())}
	data := templateLifecycleModel
	data.Database = types.StringValue("missing")
	_, found, err := readTemplateCatalog(ctx, &r.resourceClient, data)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, templateIdentity{}.Description(ctx), templateIdentity{}.MarkdownDescription(ctx))
	state := testState(t, r, templateLifecycleModel)
	response := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	templateIdentity{}.PlanModifyString(ctx, planmodifier.StringRequest{StateValue: types.StringValue("identity"), PlanValue: types.StringUnknown(), State: state, Plan: tfsdk.Plan(state)}, &response)
	require.False(t, response.Diagnostics.HasError())
	assert.Equal(t, types.StringValue("identity"), response.PlanValue)
	invalid := templateLifecycleModel
	invalid.Schema = types.StringValue("")
	_, err = dropTemplateStatement(invalid)
	require.Error(t, err)
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			c := fullCatalog()
			if operation == "create" {
				fakeState[*templateFake](c, templateFakeFamily).exists = false
			}
			r := &templateResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "ALTER TEMPLATE") {
					return nil, errors.New("alter denied")
				}
				return c.Query(ctx, target, sql, parameters)
			}))}
			planned := templateLifecycleModel
			planned.Owner = types.StringValue("other_owner")
			_, diagnostics := applyOperation(t, r, operation, templateLifecycleModel, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "alter denied")
		})
	}
	var validate resource.ValidateConfigResponse
	r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Raw: tftypes.NewValue(tftypes.String, "invalid"), Schema: state.Schema}}, &validate)
	require.True(t, validate.Diagnostics.HasError())
}
