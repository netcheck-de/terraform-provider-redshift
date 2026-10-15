package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// functionLifecycleModel is the representative function the fake catalog populates.
func functionLifecycleModel() functionModel {
	data := functionTestModel("integer")
	data.Database, data.Owner, data.Signature = types.StringValue("admin"), types.StringValue("admin"), types.StringValue("integer")
	return data
}

var (
	_ = registerLifecycleCase(lifecycleCase{name: "function", new: newFunctionResource, model: functionLifecycleModel(), absent: func(c *catalog) {
		clear(c.family("function").(*routineFamily).routines)
	}})
	_ = registerReplacementPolicy("redshift_function", map[string]replaceRule{
		"database":    replaceAlways,
		"schema":      replaceAlways,
		"name":        replaceAlways,
		"arguments":   replaceConditional("TestFunctionTypeChangesReplace"),
		"return_type": replaceConditional("TestFunctionTypeChangesReplace"),
		"language":    replaceAlways,
		"volatility":  replaceNever,
		"body":        replaceNever,
		"owner":       replaceNever,
	})
	_ = registerValidateConfigCase("function", validateConfigCase{
		new:   newFunctionResource,
		valid: functionTestModel("int", "varchar(10)"),
		invalid: func() functionModel {
			data := functionTestModel("integer")
			data.Language = types.StringValue("plpythonu")
			return data
		}(),
		unknown: func() functionModel {
			data := functionTestModel("integer")
			data.Body = types.StringUnknown()
			return data
		}(),
	})
)

// TestFunctionAlterCoverage keeps an alter step for every in-place attribute.
func TestFunctionAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newFunctionResource(), functionAlterSteps(functionSpec{}))
}

// TestFunctionTypeChangesReplace replaces the function for a different type or modifier, but not for another
// spelling or for a modifier added to a type stored without one, as an import stores it.
func TestFunctionTypeChangesReplace(t *testing.T) {
	list := func(values ...string) types.List {
		elements := make([]attr.Value, len(values))
		for i, value := range values {
			elements[i] = types.StringValue(value)
		}
		return types.ListValueMust(types.StringType, elements)
	}
	for name, test := range map[string]struct {
		before, after types.List
		replace       bool
	}{
		"spelling":   {list("int", "varchar(10)"), list("integer", "character varying(10)"), false},
		"null empty": {types.ListNull(types.StringType), list(), false},
		"type":       {list("integer"), list("bigint"), true},
		"modifier":   {list("varchar(10)"), list("varchar(20)"), true},
		"imported":   {list("integer", "character varying"), list("int", "varchar(64)"), false},
		"dropped":    {list("varchar(10)"), list("varchar"), true},
		"base":       {list("character varying"), list("char(10)"), true},
		"count":      {list("integer"), list("integer", "integer"), true},
		"unknown":    {list("integer"), types.ListUnknown(types.StringType), true},
	} {
		var resp listplanmodifier.RequiresReplaceIfFuncResponse
		functionArgumentsChanged(context.Background(), planmodifier.ListRequest{StateValue: test.before, PlanValue: test.after}, &resp)
		assert.Equal(t, test.replace, resp.RequiresReplace, name)
	}
	for name, test := range map[string]struct {
		before, after string
		replace       bool
	}{
		"spelling": {"float", "double precision", false},
		"type":     {"integer", "bigint", true},
		"invalid":  {"money", "MONEY", true},
		"imported": {"character varying", "varchar(128)", false},
		"modifier": {"varchar(64)", "varchar(128)", true},
		"to bare":  {"numeric(10,2)", "numeric", true},
		"base":     {"character varying", "char(10)", true},
	} {
		var resp stringplanmodifier.RequiresReplaceIfFuncResponse
		functionReturnTypeChanged(context.Background(), planmodifier.StringRequest{StateValue: types.StringValue(test.before), PlanValue: types.StringValue(test.after)}, &resp)
		assert.Equal(t, test.replace, resp.RequiresReplace, name)
	}
}

// functionRowClient answers the function read with rows and records statements.
func functionRowClient(rows []sqlclient.Row, statements *[]string) resourceClient {
	return testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if statements != nil {
			*statements = append(*statements, sql)
		}
		switch {
		case strings.HasPrefix(sql, "SELECT p.proname"):
			return rows, nil
		case strings.HasPrefix(sql, "SELECT database_name"):
			return []sqlclient.Row{{"database_name": "analytics"}}, nil
		}
		return nil, nil
	}))
}

// TestFunctionReadKeepsConfiguredSpelling keeps configured type and owner spellings the catalog confirms and
// reports catalog values otherwise.
func TestFunctionReadKeepsConfiguredSpelling(t *testing.T) {
	row := sqlclient.Row{"owner": "etl", "language": "sql", "volatility": "s", "return_type": "character varying", "arguments": "integer, numeric", "body": "SELECT 'x'"}
	data := functionTestModel("int4", "decimal(10,2)")
	data.ReturnType, data.Owner = types.StringValue("varchar(20)"), types.StringValue("ETL")
	found, err := (&functionResource{functionRowClient([]sqlclient.Row{row}, nil)}).read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"int4", "decimal(10,2)"}, routineStrings(data.Arguments))
	assert.Equal(t, "varchar(20)", data.ReturnType.ValueString())
	assert.Equal(t, "ETL", data.Owner.ValueString())
	assert.Equal(t, "STABLE", data.Volatility.ValueString())
	assert.Equal(t, "INTEGER, NUMERIC", data.Signature.ValueString())
	assert.Equal(t, "sql", data.Language.ValueString(), "the configured spelling of the language is kept")
	// Without a recorded fingerprint, the catalog body is surfaced.
	assert.Equal(t, "SELECT 'x'", data.Body.ValueString())
	assert.Equal(t, definitionFingerprint("SELECT 'x'"), data.DefinitionFingerprint.ValueString())

	row["return_type"], row["owner"] = "bigint", "other"
	data.DefinitionFingerprint = types.StringValue(definitionFingerprint("SELECT 'x'"))
	data.Body = types.StringValue("SELECT  'x'")
	_, err = (&functionResource{functionRowClient([]sqlclient.Row{row}, nil)}).read(context.Background(), &data)
	require.NoError(t, err)
	assert.Equal(t, "BIGINT", data.ReturnType.ValueString())
	assert.Equal(t, "other", data.Owner.ValueString())
	assert.Equal(t, "SELECT  'x'", data.Body.ValueString(), "an unchanged fingerprint keeps the configured body")
}

// TestFunctionCatalogArguments reports the catalog's types when the stored ones do not match.
func TestFunctionCatalogArguments(t *testing.T) {
	assert.Equal(t, []string{"INTEGER", "CHARACTER VARYING"}, routineStrings(functionCatalogArguments(functionTestModel("bigint").Arguments, "INTEGER, CHARACTER VARYING")))
	assert.True(t, functionCatalogArguments(functionTestModel("bigint").Arguments, "").IsNull())
	assert.True(t, functionCatalogArguments(types.ListNull(types.StringType), "").IsNull())
}

// TestFunctionReadRejectsIncompleteMetadata reports ambiguous or malformed catalog rows instead of guessing.
func TestFunctionReadRejectsIncompleteMetadata(t *testing.T) {
	valid := sqlclient.Row{"owner": "admin", "language": "sql", "volatility": "i", "return_type": "integer", "arguments": "integer", "body": "SELECT 1"}
	without := func(key, value string) sqlclient.Row {
		row := sqlclient.Row{}
		for name, current := range valid {
			row[name] = current
		}
		row[key] = value
		return row
	}
	for name, rows := range map[string][]sqlclient.Row{
		"ambiguous":   {valid, valid},
		"owner":       {without("owner", "")},
		"return type": {without("return_type", "")},
		"volatility":  {without("volatility", "x")},
	} {
		_, err := (&functionResource{functionRowClient(rows, nil)}).read(context.Background(), new(functionTestModel("integer")))
		require.Error(t, err, name)
	}
	invalid := functionTestModel("money")
	_, err := (&functionResource{functionRowClient(nil, nil)}).read(context.Background(), &invalid)
	assert.Error(t, err, "invalid stored types cannot select an overload")
}

// TestFunctionCreateRejectsInvalidDefinitionBeforeSQL validates before the first statement and state write.
func TestFunctionCreateRejectsInvalidDefinitionBeforeSQL(t *testing.T) {
	var statements []string
	r := &functionResource{functionRowClient(nil, &statements)}
	data := functionTestModel("integer")
	data.Language = types.StringValue("plpythonu")
	state := testState(t, r, data)
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "June 30, 2026")
	assert.Empty(t, statements)
	assert.True(t, resp.State.Raw.IsNull())
}

// TestFunctionCreateKeepsStateWhenVerificationFails retains the created function when the owner change or the
// catalog readback fails.
func TestFunctionCreateKeepsStateWhenVerificationFails(t *testing.T) {
	for _, failing := range []string{"ALTER FUNCTION", "SELECT p.proname"} {
		t.Run(failing, func(t *testing.T) {
			r := &functionResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, failing) {
					return nil, errors.New("injected failure")
				}
				return nil, nil
			}))}
			data := functionTestModel("integer")
			data.Database, data.Owner = types.StringValue("admin"), types.StringValue("etl")
			resp := resource.CreateResponse{State: emptyState(t, r)}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, data))}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			var observed functionModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assert.Equal(t, "INTEGER", observed.Signature.ValueString())
			assertLookupIdentity(t, observed.ID, "admin", map[string]string{"schema": "public", "name": "f_example", "arguments": "INTEGER"})
			assert.True(t, resp.State.Raw.IsFullyKnown())
		})
	}
}

// TestFunctionVerifyDetectsDivergence fails an apply whose catalog result differs from the plan.
func TestFunctionVerifyDetectsDivergence(t *testing.T) {
	row := sqlclient.Row{"owner": "admin", "language": "sql", "volatility": "v", "return_type": "integer", "arguments": "integer", "body": "SELECT 1"}
	r := &functionResource{functionRowClient([]sqlclient.Row{row}, nil)}
	data := functionTestModel("integer")
	require.ErrorContains(t, r.verify(context.Background(), new(data)), "volatility")
	data.Volatility, data.Owner = types.StringValue("VOLATILE"), types.StringValue("etl")
	require.ErrorContains(t, r.verify(context.Background(), new(data)), "owner")
	data.Owner = types.StringUnknown()
	require.NoError(t, r.verify(context.Background(), &data))
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, definitionFingerprint("SELECT 1"), data.DefinitionFingerprint.ValueString())
	assert.Equal(t, "SELECT $1 + 1", data.Body.ValueString(), "apply records the configured body")
}

// TestFunctionImportIdentity restores the overload's types from the identity and rejects identities without them.
func TestFunctionImportIdentity(t *testing.T) {
	r := &functionResource{}
	for id, expected := range map[string][]string{
		`{"workgroup_name":"w","database":"d","schema":"s","name":"f","arguments":"int4, varchar"}`: {"INTEGER", "CHARACTER VARYING"},
		`{"workgroup_name":"w","database":"d","schema":"s","name":"f","arguments":""}`:              nil,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		var arguments types.List
		require.False(t, resp.State.GetAttribute(context.Background(), path.Root("arguments"), &arguments).HasError())
		assert.Equal(t, expected, routineStrings(arguments))
	}
	for _, id := range []string{
		`{"workgroup_name":"w","database":"d","schema":"s","name":"f"}`,
		`{"workgroup_name":"w","database":"d","schema":"s","name":"f","arguments":"money"}`,
		`{"workgroup_name":"w","database":"d","name":"f","arguments":""}`,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.True(t, resp.Diagnostics.HasError(), id)
	}
}

// TestFunctionTranscripts records in-place redefinition, owner changes, drift, and overload import.
func TestFunctionTranscripts(t *testing.T) {
	base := functionLifecycleModel()
	body := base
	body.Body, body.Volatility = types.StringValue("SELECT $1 * 2"), types.StringValue("STABLE")
	owner := base
	owner.Owner = types.StringValue("etl")
	overload := functionTestModel("int", "varchar(10)")
	overload.Database = types.StringValue("admin")
	drifted := func(c *catalog) {
		c.family("function").(*routineFamily).routines[fakeRoutineKey("public", "f_example", "integer")].body = "SELECT $1 + 2"
	}
	recorded := base
	recorded.DefinitionFingerprint = types.StringValue(definitionFingerprint(base.Body.ValueString()))
	runTranscripts(t, "function_flows", newFunctionResource, []transcriptCase{
		{name: "update_body_and_volatility", operation: "update", catalog: catalogWith(), prior: base, planned: body},
		{name: "update_owner", operation: "update", catalog: catalogWith(), prior: base, planned: owner},
		{name: "read_drift", operation: "read", catalog: catalogWith(drifted), prior: recorded},
		{name: "create_overload", operation: "create", catalog: catalogWith(), planned: overload},
		{name: "import_overload", operation: "import", catalog: catalogWith(), planned: overload},
		{name: "delete_overload", operation: "delete", catalog: catalogWith(func(c *catalog) {
			c.family("function").(*routineFamily).routines[fakeRoutineKey("public", "f_example", "integer, character varying")] = &fakeRoutine{
				arguments: []fakeRoutineArgument{{mode: "IN", dataType: "integer"}, {mode: "IN", dataType: "character varying"}}, returnType: "integer", volatility: "i", body: "SELECT $1", owner: "admin",
			}
		}), prior: overload},
	})
}

// TestFunctionDriftSurfacesCatalogBody shows a body changed outside Terraform as the catalog text.
func TestFunctionDriftSurfacesCatalogBody(t *testing.T) {
	c := fullCatalog()
	c.family("function").(*routineFamily).routines[fakeRoutineKey("public", "f_example", "integer")].body = "SELECT $1 + 2"
	r := &functionResource{testResourceClient(c)}
	data := functionLifecycleModel()
	data.DefinitionFingerprint = types.StringValue(definitionFingerprint(data.Body.ValueString()))
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "SELECT $1 + 2", data.Body.ValueString())
}

// TestFunctionValidateConfigExplainsPython reports a Python UDF even while other values are unknown, invalid types
// once everything is known, and a language other than SQL; SQL itself is accepted in any case.
func TestFunctionValidateConfigExplainsPython(t *testing.T) {
	r := newFunctionResource().(*functionResource)
	python := functionTestModel("integer")
	python.Language, python.Body = types.StringValue("plpythonu"), types.StringUnknown()
	plpgsql := functionTestModel("integer")
	plpgsql.Language = types.StringValue("PLPGSQL")
	for name, data := range map[string]functionModel{"python with unknown body": python, "unknown type": functionTestModel("money"), "plpgsql": plpgsql} {
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &resp)
		assert.True(t, resp.Diagnostics.HasError(), name)
	}
	for _, language := range []string{"SQL", "sql", "Sql"} {
		data := functionTestModel("integer")
		data.Language = types.StringValue(language)
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &resp)
		assert.False(t, resp.Diagnostics.HasError(), "%s: %v", language, resp.Diagnostics)
	}
}

// TestFunctionUpdateFailures reports invalid plans before SQL, and failed or diverging redefinitions.
func TestFunctionUpdateFailures(t *testing.T) {
	prior := functionLifecycleModel()
	changed := prior
	changed.Body = types.StringValue("SELECT $1 * 2")
	invalid := changed
	invalid.ReturnType = types.StringValue("money")
	for name, test := range map[string]struct {
		planned functionModel
		fail    string
	}{
		"invalid plan":   {invalid, ""},
		"failed replace": {changed, "CREATE OR REPLACE"},
		"failed verify":  {changed, "SELECT p.proname"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			var writes []string
			r := &functionResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if test.fail != "" && strings.HasPrefix(sql, test.fail) {
					return nil, errors.New("injected failure")
				}
				if !strings.HasPrefix(sql, "SELECT") {
					writes = append(writes, sql)
				}
				return c.Query(ctx, target, sql, parameters)
			}))}
			_, diagnostics := applyOperation(t, r, "update", prior, test.planned, nil)
			require.True(t, diagnostics.HasError())
			if test.fail == "" {
				assert.Empty(t, writes)
			}
		})
	}
}
