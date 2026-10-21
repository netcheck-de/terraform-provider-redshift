package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageRoutinesPartialArguments(t *testing.T) {
	function := functionTestModel("integer")
	function.Body = types.StringUnknown()
	function.Arguments = types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown(), types.StringValue("anyelement")})
	procedure := procedureTestDefault()
	procedure.Body, procedure.Nonatomic = types.StringUnknown(), types.BoolValue(true)
	procedure.Configuration = procedureTestSettings(map[string]string{"statement_timeout": "100"})
	procedure.Arguments = types.ListValueMust(types.ObjectType{AttrTypes: procedureArgumentTypes}, []attr.Value{
		types.ObjectUnknown(procedureArgumentTypes),
		procedureArgumentList([]procedureArgumentModel{procedureTestArgument("x", "", "anyelement")}).Elements()[0],
	})
	for _, test := range []struct {
		name    string
		r       resource.Resource
		model   any
		message string
	}{
		{"function", newFunctionResource(), function, "Invalid function argument"},
		{"procedure", newProcedureResource(), procedure, "SET configuration parameters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response resource.ValidateConfigResponse
			test.r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, test.r, test.model))}, &response)
			require.True(t, response.Diagnostics.HasError())
			assert.Contains(t, fmt.Sprint(response.Diagnostics), test.message)
		})
	}
}

func TestCoverageRoutinesUnavailableDatabase(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []bool{false, true} {
		checkError := func(err error) {
			t.Helper()
			if failure {
				require.ErrorContains(t, err, "database catalog unavailable")
			} else {
				require.NoError(t, err)
			}
		}
		client := testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			if failure {
				return nil, errors.New("database catalog unavailable")
			}
			return nil, nil
		}))
		f := &functionResource{resourceClient: client}
		_, found, err := f.observe(ctx, functionTestModel("integer"))
		assert.False(t, found)
		checkError(err)
		p := &procedureResource{resourceClient: client}
		_, found, err = p.observe(ctx, procedureTestDefault())
		assert.False(t, found)
		checkError(err)
		e := &externalFunctionResource{resourceClient: client}
		data := externalFunctionTestModel()
		data.Database = types.StringValue("analytics")
		found, err = e.read(ctx, &data)
		assert.False(t, found)
		checkError(err)
		_, found, err = readExternalModelCatalog(ctx, &client, "analytics", "serving", "summarizer")
		assert.False(t, found)
		checkError(err)
	}
}

func TestCoverageRoutinesExternalKnownOptions(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*externalFunctionModel)
		message string
	}{
		{"argument limit", func(m *externalFunctionModel) {
			values := make([]string, externalFunctionMaxArguments+1)
			for i := range values {
				values[i] = "int"
			}
			m.Arguments = externalFunctionTypeValues(values)
		}, "at most"},
		{"volatility", func(m *externalFunctionModel) { m.Volatility = types.StringValue("RANDOM") }, "RANDOM"},
		{"lambda", func(m *externalFunctionModel) { m.LambdaFunction = types.StringValue("") }, "lambda_function"},
		{"role", func(m *externalFunctionModel) { m.IAMRole = types.StringValue("not-a-role") }, "IAM"},
		{"timeout", func(m *externalFunctionModel) { m.RetryTimeout = types.Int64Value(-1) }, "retry_timeout"},
		{"rows", func(m *externalFunctionModel) { m.MaxBatchRows = types.Int64Value(0) }, "max_batch_rows"},
		{"unit", func(m *externalFunctionModel) { m.MaxBatchSizeUnit = types.StringValue("GB") }, "GB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := externalFunctionWith(test.change)
			require.ErrorContains(t, externalFunctionKnownConfig(data), test.message)
		})
	}
	data := externalFunctionTestModel()
	data.Arguments = externalFunctionTypeValues([]string{"money"})
	r := &externalFunctionResource{}
	_, err := r.identityOf(data)
	require.Error(t, err)
	_, err = externalFunctionOwnerStatement(data)
	require.Error(t, err)
	_, err = dropExternalFunctionStatement(data)
	require.Error(t, err)
	data.Arguments = types.ListUnknown(types.StringType)
	assert.Nil(t, externalFunctionTypeList(data.Arguments))
	data = externalFunctionTestModel()
	before := data
	data.Owner = types.StringNull()
	statements, err := alterExternalFunctionStatements(before, data)
	require.NoError(t, err)
	assert.Empty(t, statements)
}

func TestCoverageRoutinesIdentityAndUnknownTypes(t *testing.T) {
	assert.True(t, routineDeclaredTypes(true, "integer").IsUnknown())
	assert.True(t, routineCatalogTypeMatches("FutureType", "futuretype"))
	assert.False(t, routineCatalogTypeMatches("FutureType", "other"))
	_, _, err := routineImportArguments("not JSON")
	require.ErrorContains(t, err, "expected a JSON object")
	step := routineOwnerStep(func(m functionModel) types.String { return m.Owner }, func(functionModel) sqlclient.Statement { return sqlclient.Stmt("ALTER FUNCTION").Ident("f") })
	assert.Empty(t, step.render(functionModel{}, functionModel{Owner: types.StringUnknown()}))
}

func TestCoverageRoutinesExternalRenderingFailures(t *testing.T) {
	r := newExternalFunctionResource()
	configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("ALTER refused")
	}))
	before, after := externalFunctionTestModel(), externalFunctionTestModel()
	after.LambdaFunction = types.StringValue("replacement_lambda")
	_, diagnostics := applyOperation(t, r, "update", before, after, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, fmt.Sprint(diagnostics), "ALTER refused")
	client := testResourceClient(nil)
	_, found, err := readExternalModelCatalog(context.Background(), &client, "admin", "serving", "")
	require.Error(t, err)
	assert.False(t, found)
	assert.False(t, externalModelUnread(context.Background(), nil, "iam_role"))
	for _, change := range []func(*externalModelModel){
		func(m *externalModelModel) { m.IAMRole = types.StringValue("invalid-role") },
		func(m *externalModelModel) { m.Name = types.StringValue("") },
	} {
		model := testExternalModelModel()
		change(&model)
		modelResource := newExternalModelResource()
		var response resource.ValidateConfigResponse
		modelResource.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, modelResource, model))}, &response)
		require.True(t, response.Diagnostics.HasError())
	}
}

func TestCoverageRoutinesUnknownOwnerRetainedAfterCreate(t *testing.T) {
	r := &functionResource{resourceClient: creationOnlyClient(false)}
	model := functionTestModel("integer")
	model.Owner = types.StringUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, model, nil)
	require.True(t, diagnostics.HasError())
	var actual functionModel
	require.False(t, state.Get(context.Background(), &actual).HasError())
	assert.False(t, actual.ID.IsNull())
	assert.True(t, actual.Owner.IsNull())
	assert.True(t, state.Raw.IsFullyKnown())
}
