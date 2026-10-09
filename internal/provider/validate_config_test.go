package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestValidateConfigRejectsInvalidTuplesAtPlanTime surfaces tuple errors before apply and defers unknown values.
func TestValidateConfigRejectsInvalidTuplesAtPlanTime(t *testing.T) {
	privileges := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})
	cases := map[string]struct {
		resource resource.Resource
		valid    any
		invalid  any
		// unknown would be invalid if its unknown value were read as empty.
		unknown any
	}{
		"grant": {
			newGrantResource(),
			grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("DATABASE"), Privileges: privileges},
			grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA"), Privileges: privileges},
			grantModel{DatabaseName: types.StringValue("analytics"), Role: types.StringValue("readers"), Scope: types.StringValue("SCHEMA"), SchemaName: types.StringUnknown(), Privileges: privileges},
		},
		"comment": {
			newCommentResource(),
			commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("SCHEMA"), ObjectName: types.StringValue("serving"), Text: types.StringValue("note")},
			commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("TABLE"), ObjectName: types.StringValue("t"), Text: types.StringValue("note")},
			commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("TABLE"), ObjectName: types.StringValue("t"), SchemaName: types.StringUnknown(), Text: types.StringValue("note")},
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			validator := test.resource.(resource.ResourceWithValidateConfig)
			for invalid, model := range map[bool]any{false: test.valid, true: test.invalid} {
				var resp resource.ValidateConfigResponse
				validator.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, test.resource, model))}, &resp)
				assert.Equal(t, invalid, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			}
			config := tfsdk.Config(testState(t, test.resource, test.unknown))
			var resp resource.ValidateConfigResponse
			validator.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: config}, &resp)
			assert.False(t, resp.Diagnostics.HasError(), "unknown configuration must be validated later")
		})
	}
}
