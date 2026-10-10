package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maskingLifecyclePolicy is the representative policy the masking fake populates.
var maskingLifecyclePolicy = maskingTestPolicy("mask_email", "'***'::VARCHAR(256)")

var _ = registerLifecycleCase(lifecycleCase{
	name: "masking policy", new: newMaskingPolicyResource, model: maskingLifecyclePolicy,
	absent: func(c *catalog) {
		fake := fakeState[*maskingFake](c, maskingFakeFamily)
		fake.policy, fake.attached = false, false
	},
	dependents: func(c *catalog) { fakeState[*maskingFake](c, maskingFakeFamily).attached = false },
})

var _ = registerReplacementPolicy("redshift_masking_policy", map[string]replaceRule{
	"database":     replaceAlways,
	"name":         replaceAlways,
	"input_column": replaceConditional("TestMaskingPolicyInputsReplacement"),
	"expression":   replaceNever,
})

var _ = registerValidateConfigCase("masking_policy", validateConfigCase{
	new:     newMaskingPolicyResource,
	valid:   maskingLifecyclePolicy,
	invalid: maskingTestPolicy("mask_email", "'***'::VARCHAR(256)", maskingPolicyColumn{Name: "email", Type: "VARCHAR(256)"}, maskingPolicyColumn{Name: "email", Type: "INTEGER"}),
	unknown: func() maskingPolicyModel {
		model := maskingLifecyclePolicy
		model.Expression = types.StringUnknown()
		return model
	}(),
})

// maskingPolicyCatalogClient answers the policy read with one fixed catalog row and fails on anything else.
func maskingPolicyCatalogClient(rows ...sqlclient.Row) sqlclient.Client {
	return queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "FROM svv_masking_policy") {
			return rows, nil
		}
		if strings.HasPrefix(sql, "CREATE MASKING POLICY") {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	})
}

// maskingPolicyRow is a catalog row with the given input columns and expression text.
func maskingPolicyRow(columns, expression string) sqlclient.Row {
	return sqlclient.Row{"policy_database": "admin", "policy_name": "mask_email", "input_columns": columns, "policy_expression": expression}
}

// TestMaskingPolicyTranscripts records expression changes, drift refresh, and import beyond the lifecycle flows.
func TestMaskingPolicyTranscripts(t *testing.T) {
	changed := maskingLifecyclePolicy
	changed.Expression = types.StringValue("SHA2(email, 256)")
	quoted := maskingTestPolicy(`Odd"Policy`, `'it''s \ masked'::VARCHAR(256)`)
	absent := func(c *catalog) { fakeState[*maskingFake](c, maskingFakeFamily).policy = false }
	runTranscripts(t, "masking/policy_flows", newMaskingPolicyResource, []transcriptCase{
		{name: "update_expression", operation: "update", catalog: catalogWith(), prior: maskingLifecyclePolicy, planned: changed},
		{name: "create_quoted", operation: "create", catalog: catalogWith(func(c *catalog) {
			fake := fakeState[*maskingFake](c, maskingFakeFamily)
			fake.policy, fake.attached = false, false
		}), planned: quoted},
		{name: "import_without_configuration", operation: "import", catalog: catalogWith(func(c *catalog) {
			fake := fakeState[*maskingFake](c, maskingFakeFamily)
			fake.policy, fake.attached = false, false
		}), planned: maskingLifecyclePolicy},
		{name: "delete_missing", operation: "delete", catalog: catalogWith(absent), prior: maskingLifecyclePolicy},
	})
}

// TestMaskingPolicyReadReconcilesDefinition keeps configured text and types while the catalog matches the recorded
// fingerprint, and surfaces the catalog after a change outside Terraform.
func TestMaskingPolicyReadReconcilesDefinition(t *testing.T) {
	catalogText := `CAST('***' AS TEXT)`
	row := maskingPolicyRow(`[{"colname":"email","type":"character varying(256)"}]`, `[{"expr":"CAST('***' AS TEXT)","type":"text"}]`)
	for _, test := range []struct {
		name        string
		fingerprint types.String
		expression  string
	}{
		{"matching", types.StringValue(definitionFingerprint(catalogText)), "'***'::VARCHAR(256)"},
		{"drifted", types.StringValue(definitionFingerprint("'***'")), catalogText},
		{"imported", types.StringNull(), catalogText},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newMaskingPolicyResource()
			configureTestResource(t, r, maskingPolicyCatalogClient(row))
			model := maskingLifecyclePolicy
			model.DefinitionFingerprint = test.fingerprint
			state := testState(t, r, model)
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			var observed maskingPolicyModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assert.Equal(t, test.expression, observed.Expression.ValueString())
			assert.Equal(t, definitionFingerprint(catalogText), observed.DefinitionFingerprint.ValueString())
			assert.True(t, observed.InputColumn.Equal(maskingLifecyclePolicy.InputColumn), "equivalent type spellings keep the configured one")
		})
	}
	r := newMaskingPolicyResource()
	configureTestResource(t, r, maskingPolicyCatalogClient(maskingPolicyRow(`[{"colname":"email","type":"integer"}]`, catalogText)))
	state := testState(t, r, maskingLifecyclePolicy)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var observed maskingPolicyModel
	require.False(t, resp.State.Get(context.Background(), &observed).HasError())
	assert.Equal(t, []maskingPolicyColumn{{Name: "email", Type: "integer"}}, maskingPolicyColumns(observed.InputColumn), "changed inputs surface to plan a replacement")
}

// TestMaskingPolicyCatalogFailures reports unreadable, ambiguous, or diverging catalog rows instead of guessing.
func TestMaskingPolicyCatalogFailures(t *testing.T) {
	good := maskingPolicyRow(`[{"colname":"email","type":"character varying(256)"}]`, "'***'")
	for name, test := range map[string]struct {
		rows      []sqlclient.Row
		operation string
	}{
		"malformed columns": {[]sqlclient.Row{maskingPolicyRow("not json", "'***'")}, "read"},
		"ambiguous":         {[]sqlclient.Row{good, good}, "read"},
		"diverging inputs":  {[]sqlclient.Row{maskingPolicyRow(`[{"colname":"email","type":"integer"}]`, "'***'")}, "create"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newMaskingPolicyResource()
			configureTestResource(t, r, maskingPolicyCatalogClient(test.rows...))
			assert.True(t, invoke(t, r, test.operation, maskingLifecyclePolicy, false).HasError())
		})
	}
}

// TestMaskingPolicyCreateRejectsInvalidBeforeState keeps invalid policies out of state and SQL.
func TestMaskingPolicyCreateRejectsInvalidBeforeState(t *testing.T) {
	r := newMaskingPolicyResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))
	model := maskingLifecyclePolicy
	model.Expression = types.StringValue("'***'; DROP TABLE users")
	plan := tfsdk.Plan(testState(t, r, model))
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "an invalid policy must not be recorded in state")
	assert.True(t, invoke(t, r, "update", model, false).HasError(), "update validates before ALTER")
}

// TestMaskingPolicyAbsentInputsReportedOnce leaves absent input_column blocks to the schema's IsRequired validator
// at plan time, so the user sees one error, while Create still rejects them before the first State.Set.
func TestMaskingPolicyAbsentInputsReportedOnce(t *testing.T) {
	r := newMaskingPolicyResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))
	model := maskingLifecyclePolicy
	model.InputColumn = types.ListNull(maskingPolicyColumnType)
	var validated resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &validated)
	assert.False(t, validated.Diagnostics.HasError(), "%v", validated.Diagnostics)
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, model))}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "a policy without inputs must not be recorded in state")
}

// TestMaskingPolicyImport accepts the JSON identity Create records and rejects incomplete ones.
func TestMaskingPolicyImport(t *testing.T) {
	r := newMaskingPolicyResource()
	for id, valid := range map[string]bool{
		`{"workgroup_name":"warehouse","database":"analytics","name":"mask_email"}`: true,
		`{"workgroup_name":"warehouse","database":"analytics"}`:                     false,
		`mask_email`: false,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, valid, !resp.Diagnostics.HasError(), "%s: %v", id, resp.Diagnostics)
		if valid {
			var name types.String
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("name"), &name).HasError())
			assert.Equal(t, "mask_email", name.ValueString())
			assert.False(t, resp.State.Raw.Equal(tftypes.NewValue(resp.State.Raw.Type(), nil)))
		}
	}
}

// TestMaskingPolicyInputsReplacement plans an import against a configuration that spells the input type differently
// from the catalog: the spelling alone updates in place, while a changed name, type, or unknown input replaces.
func TestMaskingPolicyInputsReplacement(t *testing.T) {
	ctx := context.Background()
	r := newMaskingPolicyResource()
	configureTestResource(t, r, catalogWith()())
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	id := r.(*maskingPolicyResource).identity("admin", map[string]string{"name": "mask_email"}).ValueString()
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	refreshed := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &refreshed)
	require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)
	var state maskingPolicyModel
	require.False(t, refreshed.State.Get(ctx, &state).HasError())
	require.Equal(t, []maskingPolicyColumn{{Name: "email", Type: "character varying(256)"}}, maskingPolicyColumns(state.InputColumn))

	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)
	modifiers := response.Schema.Blocks["input_column"].(schema.ListNestedBlock).PlanModifiers
	unknownType := types.ObjectValueMust(maskingPolicyColumnType.AttrTypes, map[string]attr.Value{"name": types.StringValue("email"), "type": types.StringUnknown()})
	for name, test := range map[string]struct {
		planned types.List
		replace bool
	}{
		"configured spelling": {maskingLifecyclePolicy.InputColumn, false},
		"folded name":         {maskingPolicyColumnList([]maskingPolicyColumn{{Name: "EMAIL", Type: "TEXT"}}), false},
		"changed type":        {maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "VARCHAR(100)"}}), true},
		"renamed input":       {maskingPolicyColumnList([]maskingPolicyColumn{{Name: "address", Type: "VARCHAR(256)"}}), true},
		"added input":         {maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "VARCHAR(256)"}, {Name: "flag", Type: "BOOLEAN"}}), true},
		"unknown type":        {types.ListValueMust(maskingPolicyColumnType, []attr.Value{unknownType}), true},
		"unknown list":        {types.ListUnknown(maskingPolicyColumnType), true},
	} {
		t.Run(name, func(t *testing.T) {
			planned := state
			planned.InputColumn, planned.Expression = test.planned, maskingLifecyclePolicy.Expression
			plan := tfsdk.Plan(testState(t, r, planned))
			if test.planned.IsUnknown() || name == "unknown type" {
				// testState cannot encode unknown values, and RequiresReplaceIf reads only the attribute values.
				plan = tfsdk.Plan(testState(t, r, state))
			}
			request := planmodifier.ListRequest{
				Path: path.Root("input_column"), State: refreshed.State, Plan: plan,
				StateValue: state.InputColumn, PlanValue: test.planned, ConfigValue: test.planned,
			}
			replace := false
			for _, modifier := range modifiers {
				resp := planmodifier.ListResponse{PlanValue: test.planned}
				modifier.PlanModifyList(ctx, request, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
				replace = replace || resp.RequiresReplace
			}
			assert.Equal(t, test.replace, replace)
		})
	}
	// The in-place update renders no statement for the inputs and keeps the configured spelling.
	statements, err := alterMaskingPolicyStatements(state, func() maskingPolicyModel {
		planned := state
		planned.InputColumn = maskingLifecyclePolicy.InputColumn
		return planned
	}())
	require.NoError(t, err)
	assert.Empty(t, statements)
	planned := state
	planned.InputColumn = maskingLifecyclePolicy.InputColumn
	updated, diagnostics := applyOperation(t, r, "update", state, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed maskingPolicyModel
	require.False(t, updated.Get(ctx, &observed).HasError())
	assert.True(t, observed.InputColumn.Equal(maskingLifecyclePolicy.InputColumn), "the update records the configured spelling")
}

// TestMaskingPolicyBlockPlan drives input_column blocks through real plans, which the model-level tests skip: the
// required-block validator, the empty plan after apply, and the in-place update for another spelling of a type.
func TestMaskingPolicyBlockPlan(t *testing.T) {
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(blocks string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_masking_policy" "email" {
  database = "admin"
  name = "mask_email"
  expression = "'***'::VARCHAR(256)"
%s
}`, blocks)
	}
	varchar := configuration(`  input_column {
    name = "email"
    type = "VARCHAR(256)"
  }`)
	text := configuration(`  input_column {
    name = "email"
    type = "TEXT"
  }`)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration(""), ExpectError: regexp.MustCompile(`Block input_column must have a configuration value`), PlanOnly: true},
			{Config: varchar, Check: testresource.TestCheckResourceAttr("redshift_masking_policy.email", "input_column.0.type", "VARCHAR(256)")},
			{Config: varchar, PlanOnly: true},
			{
				Config: text,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_masking_policy.email", plancheck.ResourceActionUpdate),
				}},
				Check: testresource.TestCheckResourceAttr("redshift_masking_policy.email", "input_column.0.type", "TEXT"),
			},
			{Config: text, PlanOnly: true},
		},
	})
}
