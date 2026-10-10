package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// procedureLifecycleModel is the representative procedure the fake catalog populates.
func procedureLifecycleModel() procedureModel {
	data := procedureTestDefault()
	data.Database, data.Owner, data.Signature = types.StringValue("admin"), types.StringValue("admin"), types.StringValue("integer")
	return data
}

var (
	_ = registerLifecycleCase(lifecycleCase{name: "procedure", new: newProcedureResource, model: procedureLifecycleModel(), absent: func(c *catalog) {
		clear(c.family("procedure").(*routineFamily).routines)
	}})
	_ = registerReplacementPolicy("redshift_procedure", map[string]replaceRule{
		"database":      replaceAlways,
		"schema":        replaceAlways,
		"name":          replaceAlways,
		"argument":      replaceConditional("TestProcedureArgumentChangesReplace"),
		"body":          replaceNever,
		"security":      replaceNever,
		"nonatomic":     replaceNever,
		"configuration": replaceNever,
		"owner":         replaceNever,
	})
	_ = registerValidateConfigCase("procedure", validateConfigCase{
		new:   newProcedureResource,
		valid: procedureTestDefault(),
		invalid: procedureTestChange(func(data *procedureModel) {
			data.Nonatomic, data.Security = types.BoolValue(true), types.StringValue("DEFINER")
		}),
		unknown: procedureTestChange(func(data *procedureModel) {
			data.Nonatomic, data.Security = types.BoolValue(true), types.StringUnknown()
		}),
	})
)

// TestProcedureAlterCoverage keeps an alter step for every in-place attribute.
func TestProcedureAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newProcedureResource(), procedureAlterSteps(procedureSpec{}))
}

// TestProcedureArgumentChangesReplace replaces the procedure for a different argument but not for another spelling.
func TestProcedureArgumentChangesReplace(t *testing.T) {
	base := procedureTestDefault().Arguments
	for name, test := range map[string]struct {
		after   types.List
		replace bool
	}{
		"spelling": {procedureTestModel(procedureTestArgument("MIN_ID", "IN", "int4"), procedureTestArgument("total", "OUT", "int8")).Arguments, false},
		"type":     {procedureTestModel(procedureTestArgument("min_id", "", "bigint"), procedureTestArgument("total", "OUT", "bigint")).Arguments, true},
		"output":   {procedureTestModel(procedureTestArgument("min_id", "", "integer")).Arguments, true},
		"unknown":  {types.ListUnknown(base.ElementType(context.Background())), true},
	} {
		var resp listplanmodifier.RequiresReplaceIfFuncResponse
		procedureArgumentsChanged(context.Background(), planmodifier.ListRequest{StateValue: base, PlanValue: test.after}, &resp)
		assert.Equal(t, test.replace, resp.RequiresReplace, name)
	}
}

// procedureRowClient answers procedure reads with fixed rows and records statements.
func procedureRowClient(row sqlclient.Row, parameters []sqlclient.Row, statements *[]string) resourceClient {
	return testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if statements != nil {
			*statements = append(*statements, sql)
		}
		switch {
		case strings.HasPrefix(sql, "SELECT p.proname") && row != nil:
			return []sqlclient.Row{row}, nil
		case strings.HasPrefix(sql, "SHOW PARAMETERS"):
			return parameters, nil
		case strings.HasPrefix(sql, "SELECT database_name"):
			return []sqlclient.Row{{"database_name": "analytics"}}, nil
		}
		return nil, nil
	}))
}

// TestProcedureReadReconcilesArguments keeps configured arguments the catalog confirms, including an OUT argument
// whose name the catalog does not keep, and reports the catalog's arguments otherwise.
func TestProcedureReadReconcilesArguments(t *testing.T) {
	row := sqlclient.Row{"owner": "admin", "security_definer": "true", "arguments": "integer", "body": "BEGIN NULL; END;"}
	parameters := []sqlclient.Row{
		{"parameter_name": "", "ordinal_position": "2", "parameter_type": "OUT", "data_type": "bigint"},
		{"parameter_name": "min_id", "ordinal_position": "1", "parameter_type": "IN", "data_type": "integer"},
	}
	data := procedureTestModel(procedureTestArgument("MIN_ID", "IN", "int"), procedureTestArgument("total", "OUT", "int8"))
	found, err := (&procedureResource{procedureRowClient(row, parameters, nil)}).read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	arguments := procedureArguments(data.Arguments)
	require.Len(t, arguments, 2)
	assert.Equal(t, "MIN_ID", arguments[0].Name.ValueString())
	assert.Equal(t, "int8", arguments[1].Type.ValueString())
	assert.Equal(t, "DEFINER", data.Security.ValueString())
	assert.Equal(t, "integer", data.Signature.ValueString())

	parameters[1]["parameter_type"] = "INOUT"
	found, err = (&procedureResource{procedureRowClient(row, parameters, nil)}).read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	arguments = procedureArguments(data.Arguments)
	require.Len(t, arguments, 2)
	assert.Equal(t, "INOUT", arguments[0].Mode.ValueString())
	assert.Equal(t, "integer", arguments[0].Type.ValueString())
	assert.True(t, arguments[1].Name.IsNull())
	assert.Equal(t, "OUT", arguments[1].Mode.ValueString())
}

// TestProcedureReadRejectsIncompleteMetadata reports malformed catalog rows and parameter failures.
func TestProcedureReadRejectsIncompleteMetadata(t *testing.T) {
	valid := sqlclient.Row{"owner": "admin", "security_definer": "false", "arguments": "integer", "body": "BEGIN NULL; END;"}
	parameter := []sqlclient.Row{{"parameter_name": "a", "ordinal_position": "1", "parameter_type": "IN", "data_type": "integer"}}
	for name, test := range map[string]struct {
		row        sqlclient.Row
		parameters []sqlclient.Row
	}{
		"owner":    {sqlclient.Row{"owner": "", "security_definer": "false"}, parameter},
		"security": {sqlclient.Row{"owner": "admin", "security_definer": "maybe"}, parameter},
		"position": {valid, []sqlclient.Row{{"parameter_name": "a", "ordinal_position": "x", "parameter_type": "IN", "data_type": "integer"}}},
		"type":     {valid, []sqlclient.Row{{"parameter_name": "a", "ordinal_position": "1", "parameter_type": "IN", "data_type": ""}}},
	} {
		data := procedureTestDefault()
		_, err := (&procedureResource{procedureRowClient(test.row, test.parameters, nil)}).read(context.Background(), &data)
		require.Error(t, err, name)
	}
	failing := &procedureResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SHOW PARAMETERS"):
			return nil, errors.New("show failed")
		case strings.HasPrefix(sql, "SELECT p.proname"):
			return []sqlclient.Row{valid}, nil
		}
		return []sqlclient.Row{{"database_name": "analytics"}}, nil
	}))}
	data := procedureTestDefault()
	_, err := failing.read(context.Background(), &data)
	require.ErrorContains(t, err, "show failed")
	// SHOW PARAMETERS reports the RETURN row only for functions; a procedure ignores it.
	arguments, err := procedureParameters([]sqlclient.Row{{"parameter_type": "RETURN", "ordinal_position": "0", "data_type": "integer"}})
	require.NoError(t, err)
	assert.Empty(t, arguments)
}

// TestProcedureCreateRejectsInvalidDefinitionBeforeSQL validates before the first statement and state write.
func TestProcedureCreateRejectsInvalidDefinitionBeforeSQL(t *testing.T) {
	var statements []string
	r := &procedureResource{procedureRowClient(nil, nil, &statements)}
	data := procedureTestChange(func(data *procedureModel) {
		data.Nonatomic, data.Configuration = types.BoolValue(true), procedureTestSettings(map[string]string{"search_path": "x"})
	})
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, data))}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Empty(t, statements)
	assert.True(t, resp.State.Raw.IsNull())
}

// TestProcedureCreateKeepsStateWhenVerificationFails retains the created procedure when a later step fails.
func TestProcedureCreateKeepsStateWhenVerificationFails(t *testing.T) {
	for _, failing := range []string{"ALTER PROCEDURE", "SELECT p.proname"} {
		t.Run(failing, func(t *testing.T) {
			r := &procedureResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, failing) {
					return nil, errors.New("injected failure")
				}
				return nil, nil
			}))}
			data := procedureTestDefault()
			data.Database, data.Owner = types.StringValue("admin"), types.StringValue("etl")
			resp := resource.CreateResponse{State: emptyState(t, r)}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, data))}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			var observed procedureModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assertLookupIdentity(t, observed.ID, "admin", map[string]string{"schema": "public", "name": "sp_example", "arguments": "integer"})
			assert.True(t, resp.State.Raw.IsFullyKnown())
		})
	}
}

// TestProcedureVerifyDetectsDivergence fails an apply whose security mode or owner did not converge.
func TestProcedureVerifyDetectsDivergence(t *testing.T) {
	row := sqlclient.Row{"owner": "admin", "security_definer": "false", "arguments": "integer", "body": "BEGIN NULL; END;"}
	parameters := []sqlclient.Row{{"parameter_name": "min_id", "ordinal_position": "1", "parameter_type": "IN", "data_type": "integer"}, {"parameter_name": "total", "ordinal_position": "2", "parameter_type": "OUT", "data_type": "bigint"}}
	r := &procedureResource{procedureRowClient(row, parameters, nil)}
	data := procedureTestChange(func(data *procedureModel) { data.Security = types.StringValue("DEFINER") })
	require.ErrorContains(t, r.verify(context.Background(), new(data)), "security")
	data.Security, data.Owner = types.StringValue("INVOKER"), types.StringValue("etl")
	require.ErrorContains(t, r.verify(context.Background(), new(data)), "owner")
	data.Owner = types.StringUnknown()
	require.NoError(t, r.verify(context.Background(), &data))
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, definitionFingerprint("BEGIN NULL; END;"), data.DefinitionFingerprint.ValueString())
}

// TestProcedureImportIdentity imports the input types as IN arguments and rejects identities without them.
func TestProcedureImportIdentity(t *testing.T) {
	r := &procedureResource{}
	resp := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"w","database":"d","schema":"s","name":"p","arguments":"int, varchar"}`}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var arguments types.List
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("argument"), &arguments).HasError())
	imported := procedureArguments(arguments)
	require.Len(t, imported, 2)
	assert.Equal(t, "character varying", imported[1].Type.ValueString())
	assert.True(t, imported[1].Mode.IsNull())
	for _, id := range []string{`{"workgroup_name":"w","database":"d","schema":"s","name":"p"}`, `not json`} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.True(t, resp.Diagnostics.HasError(), id)
	}
}

// TestProcedureTranscripts records redefinition, unobservable settings, owner changes, and import.
func TestProcedureTranscripts(t *testing.T) {
	base := procedureLifecycleModel()
	definer := base
	definer.Body, definer.Security = types.StringValue("BEGIN total := min_id; END;"), types.StringValue("DEFINER")
	definer.Configuration = procedureTestSettings(map[string]string{"search_path": "public"})
	nonatomic := base
	nonatomic.Nonatomic = types.BoolValue(true)
	owner := base
	owner.Owner = types.StringValue("etl")
	noArguments := procedureTestModel()
	noArguments.Database, noArguments.Name, noArguments.Body = types.StringValue("admin"), types.StringValue("sp_refresh"), types.StringValue("BEGIN NULL; END;")
	runTranscripts(t, "procedure_flows", newProcedureResource, []transcriptCase{
		{name: "update_body_security_settings", operation: "update", catalog: catalogWith(), prior: base, planned: definer},
		{name: "update_nonatomic", operation: "update", catalog: catalogWith(), prior: base, planned: nonatomic},
		{name: "update_owner", operation: "update", catalog: catalogWith(), prior: base, planned: owner},
		{name: "create_no_arguments", operation: "create", catalog: catalogWith(), planned: noArguments},
		{name: "import_no_arguments", operation: "import", catalog: catalogWith(), planned: noArguments},
	})
}

// TestProcedureUpdateFailures reports invalid plans before SQL, and failed redefinitions or verifications.
func TestProcedureUpdateFailures(t *testing.T) {
	prior := procedureLifecycleModel()
	changed := prior
	changed.Security = types.StringValue("DEFINER")
	invalid := changed
	invalid.Nonatomic = types.BoolValue(true)
	for name, test := range map[string]struct {
		planned procedureModel
		fail    string
	}{
		"invalid plan":   {invalid, ""},
		"failed replace": {changed, "CREATE OR REPLACE"},
		"failed verify":  {changed, "SHOW PARAMETERS"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			var writes []string
			r := &procedureResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if test.fail != "" && strings.HasPrefix(sql, test.fail) {
					return nil, errors.New("injected failure")
				}
				if !fakeRead(sql) {
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

// TestProcedureArgumentBlockPlans plans argument blocks through Terraform: no diff after create, including for a
// procedure without blocks, a lookup selecting the overload from the IN and INOUT blocks, an in-place update for a
// respelled argument, an import, and a replacement for a changed type.
func TestProcedureArgumentBlockPlans(t *testing.T) {
	configuration := func(factor, factorType string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
resource "redshift_procedure" "scale" {
  database = "admin"
  schema   = "public"
  name     = "sp_plan_scale"
  argument {
    name = %q
    type = %q
  }
  argument {
    name = "amount"
    mode = "INOUT"
    type = "bigint"
  }
  argument {
    name = "label"
    mode = "OUT"
    type = "varchar(64)"
  }
  body = "BEGIN amount := amount * factor; label := 'scaled'; END;"
}
resource "redshift_procedure" "refresh" {
  database = "admin"
  schema   = "public"
  name     = "sp_plan_refresh"
  body     = "BEGIN NULL; END;"
}
data "redshift_procedure" "scale" {
  database  = redshift_procedure.scale.database
  schema    = redshift_procedure.scale.schema
  name      = redshift_procedure.scale.name
  arguments = [for argument in redshift_procedure.scale.argument : argument.type if argument.mode != "OUT"]
}`, factor, factorType)
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: fullCatalog()})},
		Steps: []testresource.TestStep{
			{Config: configuration("factor", "int"), Check: testresource.ComposeAggregateTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "signature", "integer, bigint"),
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "argument.#", "3"),
				testresource.TestCheckResourceAttr("redshift_procedure.scale", "argument.0.type", "int"),
				testresource.TestCheckResourceAttr("redshift_procedure.refresh", "argument.#", "0"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "arguments.#", "2"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.#", "3"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.0.type", "integer"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.mode", "OUT"),
				testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.type", "character varying"),
			)},
			{Config: configuration("factor", "int"), PlanOnly: true},
			{Config: configuration("FACTOR", "integer"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_procedure.scale", plancheck.ResourceActionUpdate),
				plancheck.ExpectResourceAction("redshift_procedure.refresh", plancheck.ResourceActionNoop),
			}}},
			{Config: configuration("FACTOR", "integer"), PlanOnly: true},
			// The catalog keeps the folded name and the type without its length.
			{ResourceName: "redshift_procedure.scale", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"argument.0.name", "argument.2.type"}},
			{Config: configuration("factor", "bigint"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_procedure.scale", plancheck.ResourceActionDestroyBeforeCreate),
			}}, Check: testresource.TestCheckResourceAttr("data.redshift_procedure.scale", "signature", "bigint, bigint")},
			{Config: configuration("factor", "bigint"), PlanOnly: true},
		},
	})
}
