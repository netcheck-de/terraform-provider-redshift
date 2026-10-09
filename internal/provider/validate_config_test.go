package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateConfigCase supplies plan-time tuple validation models for one resource.
type validateConfigCase struct {
	// new constructs the resource under test.
	new func() resource.Resource
	// valid must pass validation.
	valid any
	// invalid must fail validation.
	invalid any
	// unknown would be invalid if its unknown value were read as empty.
	unknown any
}

// validateConfigCases holds the cases each type's test file registers.
var validateConfigCases testRegistry[validateConfigCase]

// registerValidateConfigCase declares a resource's ValidateConfig case from its own test file.
func registerValidateConfigCase(name string, test validateConfigCase) bool {
	return validateConfigCases.add(name, test)
}

// TestValidateConfigRejectsInvalidTuplesAtPlanTime surfaces tuple errors before apply and defers unknown values.
func TestValidateConfigRejectsInvalidTuplesAtPlanTime(t *testing.T) {
	require.Empty(t, validateConfigCases.duplicates, "duplicate ValidateConfig cases")
	require.NotEmpty(t, validateConfigCases.entries)
	for _, name := range validateConfigCases.keys() {
		test := validateConfigCases.entries[name]
		t.Run(name, func(t *testing.T) {
			r := test.new()
			validator, ok := r.(resource.ResourceWithValidateConfig)
			require.True(t, ok, "resource does not implement ValidateConfig")
			for invalid, model := range map[bool]any{false: test.valid, true: test.invalid} {
				var resp resource.ValidateConfigResponse
				validator.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &resp)
				assert.Equal(t, invalid, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			}
			config := tfsdk.Config(testState(t, r, test.unknown))
			var resp resource.ValidateConfigResponse
			validator.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: config}, &resp)
			assert.False(t, resp.Diagnostics.HasError(), "unknown configuration must be validated later")
		})
	}
}
