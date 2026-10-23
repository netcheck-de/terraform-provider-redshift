package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// Unrelated references must not hide invalid settings that are already known during planning.
func TestFunctionFamiliesPartialKnownValidation(t *testing.T) {
	function := functionTestModel("integer")
	function.Body, function.ReturnType = types.StringUnknown(), types.StringValue("money")
	procedure := procedureTestDefault()
	procedure.Body, procedure.Nonatomic, procedure.Security = types.StringUnknown(), types.BoolValue(true), types.StringValue("DEFINER")
	model := testExternalModelModel()
	model.ModelID, model.RequestType, model.ResponseType = types.StringUnknown(), types.StringValue("RAW"), types.StringValue("SUPER")
	table := externalTableTestModel()
	table.Location, table.InputFormat = types.StringUnknown(), types.StringValue("input")
	rls := rlsPolicyLifecycleModel()
	rls.Name, rls.Predicate = types.StringUnknown(), types.StringValue("x; DROP TABLE t")
	masking := maskingLifecyclePolicy
	masking.Name, masking.Expression = types.StringUnknown(), types.StringValue("x; DROP TABLE t")
	for _, test := range []struct {
		name string
		new  func() resource.Resource
		data any
	}{
		{"function", newFunctionResource, function},
		{"procedure", newProcedureResource, procedure},
		{"external_model", newExternalModelResource, model},
		{"external_table", newExternalTableResource, table},
		{"rls_policy", newRlsPolicyResource, rls},
		{"masking_policy", newMaskingPolicyResource, masking},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := test.new()
			var response resource.ValidateConfigResponse
			r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, test.data))}, &response)
			assert.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		})
	}
}
