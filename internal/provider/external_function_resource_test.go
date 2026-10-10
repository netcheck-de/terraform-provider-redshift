package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "external_function", new: newExternalFunctionResource, model: externalFunctionTestModel(),
	absent: func(c *catalog) { fakeState[*externalFunctionFake](c, "external_function").exists = false },
})

var _ = registerReplacementPolicy("redshift_external_function", map[string]replaceRule{
	"database":            replaceAlways,
	"schema":              replaceAlways,
	"name":                replaceAlways,
	"arguments":           replaceConditional("TestExternalFunctionArgumentsReplacement"),
	"return_type":         replaceConditional("TestExternalFunctionReturnTypeReplacement"),
	"volatility":          replaceNever,
	"lambda_function":     replaceNever,
	"iam_role":            replaceNever,
	"retry_timeout":       replaceNever,
	"max_batch_rows":      replaceNever,
	"max_batch_size":      replaceNever,
	"max_batch_size_unit": replaceNever,
	"owner":               replaceNever,
})

var _ = registerLookupExemption("redshift_external_function", "no documented catalog view reports the Lambda function, IAM role, or batch options, so a single-object lookup could not mirror the resource; redshift_functions lists its signature, return type, volatility, language, and owner")

var _ = registerValidateConfigCase("external_function", validateConfigCase{
	new:     newExternalFunctionResource,
	valid:   externalFunctionTestModel(),
	invalid: externalFunctionWith(func(data *externalFunctionModel) { data.ReturnType = types.StringValue("super") }),
	unknown: externalFunctionWith(func(data *externalFunctionModel) {
		data.ReturnType, data.IAMRole = types.StringValue("super"), types.StringUnknown()
	}),
})

// externalFunctionPlanAttribute returns one attribute of the resource schema.
func externalFunctionPlanAttribute(t *testing.T, name string) schema.Attribute {
	t.Helper()
	var response resource.SchemaResponse
	newExternalFunctionResource().Schema(context.Background(), resource.SchemaRequest{}, &response)
	attribute, ok := response.Schema.Attributes[name]
	require.True(t, ok, name)
	return attribute
}

// TestExternalFunctionArgumentsReplacement replaces the function for a new signature but not for another
// spelling of the same types.
func TestExternalFunctionArgumentsReplacement(t *testing.T) {
	attribute := externalFunctionPlanAttribute(t, "arguments")
	for _, test := range []struct {
		name          string
		before, after []string
		replace       bool
	}{
		{"spelling", []string{"int", "varchar(10)"}, []string{"integer", "character varying"}, false},
		{"length", []string{"varchar(10)"}, []string{"varchar(20)"}, false},
		{"type", []string{"int"}, []string{"bigint"}, true},
		{"added", []string{"int"}, []string{"int", "int"}, true},
		{"order", []string{"int", "date"}, []string{"date", "int"}, true},
		{"invalid", []string{"int"}, []string{"hstore"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := externalFunctionTypeValues(test.before), externalFunctionTypeValues(test.after)
			assert.Equal(t, test.replace, externalFunctionReplaces(t, attribute, before, after))
		})
	}
	unknown := types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})
	assert.True(t, externalFunctionReplaces(t, attribute, externalFunctionTypeValues([]string{"int"}), unknown), "an unknown element may change the signature")
	assert.True(t, externalFunctionReplaces(t, attribute, externalFunctionTypeValues([]string{}), types.ListUnknown(types.StringType)), "an unknown list may resolve to a new signature")
	assert.True(t, externalFunctionReplaces(t, attribute, externalFunctionTypeValues([]string{"int"}), types.ListUnknown(types.StringType)), "an unknown list may resolve to a new signature")
}

// TestExternalFunctionReturnTypeReplacement replaces the function only when the base result type changes.
func TestExternalFunctionReturnTypeReplacement(t *testing.T) {
	attribute := externalFunctionPlanAttribute(t, "return_type")
	for _, test := range []struct {
		before, after string
		replace       bool
	}{
		{"int", "integer", false},
		{"varchar(10)", "varchar(512)", false},
		{"varchar", "int", true},
		{"int", "super", true},
	} {
		t.Run(test.before+"_"+test.after, func(t *testing.T) {
			assert.Equal(t, test.replace, externalFunctionReplaces(t, attribute, types.StringValue(test.before), types.StringValue(test.after)))
		})
	}
}

// TestExternalFunctionReadReportsMetadata keeps configured spellings and Lambda options while refreshing the
// observable catalog state.
func TestExternalFunctionReadReportsMetadata(t *testing.T) {
	c := fullCatalog()
	r := &externalFunctionResource{testResourceClient(c)}
	data := externalFunctionWith(func(data *externalFunctionModel) {
		data.Arguments, data.ReturnType, data.Owner = externalFunctionTypeValues([]string{"text"}), types.StringValue("varchar(64)"), types.StringNull()
	})
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, externalFunctionTypeValues([]string{"text"}), data.Arguments)
	assert.Equal(t, "varchar(64)", data.ReturnType.ValueString())
	assert.Equal(t, "STABLE", data.Volatility.ValueString())
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, "exfunc_upper", data.LambdaFunction.ValueString(), "Lambda options are kept from state")

	data.ReturnType = types.StringValue("int")
	_, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.Equal(t, "CHARACTER VARYING", data.ReturnType.ValueString(), "a different base type surfaces as drift in uppercase")
}

// TestExternalFunctionReadRejectsUnexpectedMetadata fails on incomplete, ambiguous, or non-Lambda catalog rows.
func TestExternalFunctionReadRejectsUnexpectedMetadata(t *testing.T) {
	row := func(change func(dataapi.Row)) []dataapi.Row {
		value := (&externalFunctionFake{exists: true, owner: "admin", volatility: "v", language: "exfunc"}).functionRow()
		change(value)
		return []dataapi.Row{value}
	}
	for name, rows := range map[string][]dataapi.Row{
		"missing owner":    row(func(value dataapi.Row) { value["owner"] = "" }),
		"missing type":     row(func(value dataapi.Row) { value["return_type"] = "" }),
		"sql function":     row(func(value dataapi.Row) { value["language"] = "sql" }),
		"volatility":       row(func(value dataapi.Row) { value["volatility"] = "x" }),
		"ambiguous result": append(row(func(dataapi.Row) {}), row(func(dataapi.Row) {})...),
	} {
		t.Run(name, func(t *testing.T) {
			r := &externalFunctionResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return rows, nil
			}))}
			data := externalFunctionTestModel()
			_, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
	invalid := externalFunctionWith(func(data *externalFunctionModel) { data.Arguments = externalFunctionTypeValues([]string{"hstore"}) })
	_, err := (&externalFunctionResource{testResourceClient(fullCatalog())}).read(context.Background(), &invalid)
	require.Error(t, err, "state with an invalid signature cannot be read")
}

// TestExternalFunctionCreateRejectsInvalidDefinitionBeforeSQL fails before any statement or state is written.
func TestExternalFunctionCreateRejectsInvalidDefinitionBeforeSQL(t *testing.T) {
	c := fullCatalog()
	fakeState[*externalFunctionFake](c, "external_function").exists = false
	r := newExternalFunctionResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "create", nil, externalFunctionWith(func(data *externalFunctionModel) { data.IAMRole = types.StringValue("role") }), nil)
	require.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull(), "no state may be recorded for a rejected definition")
	assert.Empty(t, c.writes)
}

// TestExternalFunctionCreationErrorsRetainKnownState keeps the created function in state when the owner change
// or the verification fails after CREATE succeeded.
func TestExternalFunctionCreationErrorsRetainKnownState(t *testing.T) {
	for _, failing := range []string{"ALTER FUNCTION", "SELECT"} {
		t.Run(failing, func(t *testing.T) {
			c := fullCatalog()
			fakeState[*externalFunctionFake](c, "external_function").exists = false
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, failing) {
					return nil, errors.New("injected failure")
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newExternalFunctionResource()
			configureTestResource(t, r, client)
			planned := externalFunctionWith(func(data *externalFunctionModel) { data.Owner = types.StringUnknown() })
			if failing == "ALTER FUNCTION" {
				planned.Owner = types.StringValue("etl")
			}
			state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
			require.True(t, diagnostics.HasError())
			require.True(t, state.Raw.IsFullyKnown())
			var observed externalFunctionModel
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.False(t, observed.ID.IsNull())
		})
	}
}

// TestExternalFunctionVerifiesConvergence fails when the catalog does not show the planned volatility or owner.
func TestExternalFunctionVerifiesConvergence(t *testing.T) {
	for name, change := range map[string]func(*externalFunctionModel){
		"volatility": func(data *externalFunctionModel) { data.Volatility = types.StringValue("VOLATILE") },
		"owner":      func(data *externalFunctionModel) { data.Owner = types.StringValue("etl") },
		"returns":    func(data *externalFunctionModel) { data.ReturnType = types.StringValue("int") },
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "CREATE OR REPLACE") || strings.HasPrefix(sql, "ALTER FUNCTION") {
					return nil, nil // Acknowledged without effect.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newExternalFunctionResource()
			configureTestResource(t, r, client)
			_, diagnostics := applyOperation(t, r, "update", externalFunctionTestModel(), externalFunctionWith(change), nil)
			require.True(t, diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestExternalFunctionUpdateRejectsInvalidPlan reports an invalid definition without executing SQL.
func TestExternalFunctionUpdateRejectsInvalidPlan(t *testing.T) {
	c := fullCatalog()
	r := newExternalFunctionResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "update", externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) { data.MaxBatchRows = types.Int64Value(0) }), nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestExternalFunctionTranscripts records restating, owner changes, a function without arguments, and import.
func TestExternalFunctionTranscripts(t *testing.T) {
	moved := externalFunctionWith(func(data *externalFunctionModel) {
		data.LambdaFunction, data.Volatility, data.RetryTimeout = types.StringValue("exfunc_upper_v2"), types.StringValue("VOLATILE"), types.Int64Value(0)
		data.Owner = types.StringValue("etl")
	})
	runTranscripts(t, "flows/external_function", newExternalFunctionResource, []transcriptCase{
		{name: "update_definition_and_owner", operation: "update", catalog: catalogWith(), prior: externalFunctionTestModel(), planned: moved},
		{name: "update_spelling_only", operation: "update", catalog: catalogWith(), prior: externalFunctionTestModel(), planned: externalFunctionWith(func(data *externalFunctionModel) {
			data.Arguments, data.ReturnType = externalFunctionTypeValues([]string{"text"}), types.StringValue("text")
		})},
		{name: "create_without_owner", operation: "create", catalog: catalogWith(func(c *catalog) { fakeState[*externalFunctionFake](c, "external_function").exists = false }), planned: externalFunctionWith(func(data *externalFunctionModel) {
			data.Owner = types.StringUnknown()
		})},
	})
}

// TestExternalFunctionImport restores every identity field, including an empty signature.
func TestExternalFunctionImport(t *testing.T) {
	r := newExternalFunctionResource()
	importID := func(signature string) string {
		return fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"f_exfunc_upper","arguments":%q}`, signature)
	}
	for signature, expected := range map[string][]string{"CHARACTER VARYING": {"CHARACTER VARYING"}, "": {}, "INTEGER, TIMESTAMP WITHOUT TIME ZONE": {"INTEGER", "TIMESTAMP WITHOUT TIME ZONE"}} {
		response := resource.ImportStateResponse{State: emptyState(t, r)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: importID(signature)}, &response)
		require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		var argumentTypes types.List
		require.False(t, response.State.GetAttribute(context.Background(), path.Root("arguments"), &argumentTypes).HasError())
		assert.Equal(t, externalFunctionTypeValues(expected), argumentTypes)
	}
	for _, id := range []string{
		`{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"f"}`,
		importID("varchar"),
		importID("character varying"),
		importID("INTEGER,INTEGER"),
		`{"workgroup_name":"warehouse","database":"admin","name":"f","arguments":""}`,
	} {
		response := resource.ImportStateResponse{State: emptyState(t, r)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &response)
		assert.True(t, response.Diagnostics.HasError(), id)
	}
}

// TestExternalFunctionImportMatchesCreatedIdentity reads an imported function with the identity Create recorded.
func TestExternalFunctionImportMatchesCreatedIdentity(t *testing.T) {
	c := fullCatalog()
	fakeState[*externalFunctionFake](c, "external_function").exists = false
	r := newExternalFunctionResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "create", nil, externalFunctionWith(func(data *externalFunctionModel) {
		data.Arguments = externalFunctionTypeValues([]string{"varchar(32)"})
	}), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var id types.String
	require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"f_exfunc_upper","arguments":"CHARACTER VARYING"}`, id.ValueString())
	require.False(t, importAndRead(t, r, id.ValueString()).HasError())
}

// externalFunctionReplaces runs an attribute's plan modifiers for an update from before to after and reports
// whether any requests replacement.
func externalFunctionReplaces(t *testing.T, attribute schema.Attribute, before, after attr.Value) bool {
	t.Helper()
	ctx := context.Background()
	single := schema.Schema{Attributes: map[string]schema.Attribute{"value": attribute}}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"value": attribute.GetType().TerraformType(ctx)}}
	raw := func(value attr.Value) tftypes.Value {
		converted, err := value.ToTerraformValue(ctx)
		require.NoError(t, err)
		return tftypes.NewValue(objectType, map[string]tftypes.Value{"value": converted})
	}
	plan := tfsdk.Plan{Schema: single, Raw: raw(after)}
	state := tfsdk.State{Schema: single, Raw: raw(before)}
	config := tfsdk.Config{Schema: single, Raw: plan.Raw}
	root := path.Root("value")
	replace := false
	switch attribute := attribute.(type) {
	case schema.ListAttribute:
		for _, modifier := range attribute.PlanModifiers {
			request := planmodifier.ListRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.List), PlanValue: after.(types.List), StateValue: before.(types.List)}
			response := planmodifier.ListResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyList(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	case schema.StringAttribute:
		for _, modifier := range attribute.PlanModifiers {
			request := planmodifier.StringRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.String), PlanValue: after.(types.String), StateValue: before.(types.String)}
			response := planmodifier.StringResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyString(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	default:
		t.Fatalf("unsupported attribute %T", attribute)
	}
	return replace
}
