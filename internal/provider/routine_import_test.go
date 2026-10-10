package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routineImportState creates the configured routine in the fake behind r, then imports and refreshes it from the
// created identity, as terraform import does, and returns the imported state.
func routineImportState[M any](t *testing.T, r resource.Resource, configured M) M {
	t.Helper()
	ctx := context.Background()
	created, diagnostics := applyOperation(t, r, "create", nil, configured, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var id types.String
	require.False(t, created.GetAttribute(ctx, path.Root("id"), &id).HasError())
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id.ValueString()}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	var data M
	require.False(t, read.State.Get(ctx, &data).HasError())
	return data
}

// routineStatements returns the recorded statements that start with prefix.
func routineStatements(recorder *recordingClient, prefix string) []string {
	var statements []string
	for _, entry := range recorder.take() {
		if strings.HasPrefix(entry.sql, prefix) {
			statements = append(statements, entry.sql)
		}
	}
	return statements
}

// TestFunctionImportKeepsModifiedTypes plans the imported function against the configuration that declared
// varchar(n) types. The catalog reports them without modifiers, so the plan must update in place, restating the
// definition, rather than drop a function that views or grants depend on.
func TestFunctionImportKeepsModifiedTypes(t *testing.T) {
	configured := functionTestModel("int", "varchar(64)")
	configured.Database, configured.ReturnType = types.StringValue("admin"), types.StringValue("varchar(128)")
	configured.Body = types.StringValue("SELECT $2 || '-' || $1::varchar")
	recorder := &recordingClient{client: fullCatalog()}
	r := newFunctionResource()
	configureTestResource(t, r, recorder)
	imported := routineImportState(t, r, configured)
	require.Equal(t, []string{"integer", "character varying"}, routineStrings(imported.Arguments))
	require.Equal(t, "character varying", imported.ReturnType.ValueString())

	var arguments listplanmodifier.RequiresReplaceIfFuncResponse
	functionArgumentsChanged(context.Background(), planmodifier.ListRequest{StateValue: imported.Arguments, PlanValue: configured.Arguments}, &arguments)
	assert.False(t, arguments.RequiresReplace, "arguments")
	var returns stringplanmodifier.RequiresReplaceIfFuncResponse
	functionReturnTypeChanged(context.Background(), planmodifier.StringRequest{StateValue: imported.ReturnType, PlanValue: configured.ReturnType}, &returns)
	assert.False(t, returns.RequiresReplace, "return_type")

	planned := configured
	planned.ID, planned.Signature, planned.Owner = imported.ID, imported.Signature, imported.Owner
	planned.DefinitionFingerprint = types.StringUnknown()
	recorder.take()
	updated, diagnostics := applyOperation(t, r, "update", imported, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{`CREATE OR REPLACE FUNCTION "public"."f_example"(integer, character varying(64)) RETURNS character varying(128) IMMUTABLE AS $$SELECT $2 || '-' || $1::varchar$$ LANGUAGE sql`},
		routineStatements(recorder, "CREATE"))
	var state functionModel
	require.False(t, updated.Get(context.Background(), &state).HasError())
	assert.Equal(t, []string{"int", "varchar(64)"}, routineStrings(state.Arguments))
	assert.Equal(t, "varchar(128)", state.ReturnType.ValueString())
}

// TestProcedureImportKeepsModifiedTypes plans the imported procedure against the configuration that declared a
// varchar(n) OUT argument, which SHOW PARAMETERS reports as data_type without the length.
func TestProcedureImportKeepsModifiedTypes(t *testing.T) {
	configured := procedureTestModel(procedureTestArgument("factor", "", "integer"), procedureTestArgument("label", "OUT", "varchar(64)"))
	configured.Database, configured.Name = types.StringValue("admin"), types.StringValue("sp_label")
	recorder := &recordingClient{client: fullCatalog()}
	r := newProcedureResource()
	configureTestResource(t, r, recorder)
	imported := routineImportState(t, r, configured)
	importedArguments := procedureArguments(imported.Arguments)
	require.Len(t, importedArguments, 2)
	require.Equal(t, "character varying", importedArguments[1].Type.ValueString())

	var arguments listplanmodifier.RequiresReplaceIfFuncResponse
	procedureArgumentsChanged(context.Background(), planmodifier.ListRequest{StateValue: imported.Arguments, PlanValue: configured.Arguments}, &arguments)
	assert.False(t, arguments.RequiresReplace)

	planned := configured
	planned.ID, planned.Signature, planned.Owner = imported.ID, imported.Signature, imported.Owner
	planned.DefinitionFingerprint = types.StringUnknown()
	recorder.take()
	updated, diagnostics := applyOperation(t, r, "update", imported, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	statements := routineStatements(recorder, "CREATE")
	require.Len(t, statements, 1)
	assert.Contains(t, statements[0], `("factor" IN integer, "label" OUT character varying(64))`)
	var state procedureModel
	require.False(t, updated.Get(context.Background(), &state).HasError())
	assert.Equal(t, "varchar(64)", procedureArguments(state.Arguments)[1].Type.ValueString())
}
