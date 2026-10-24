package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewFamiliesPartialKnownValidation exercises planning without any SQL or fully-known configuration gate.
func TestViewFamiliesPartialKnownValidation(t *testing.T) {
	type validationCase struct {
		name    string
		values  map[string]attr.Value
		invalid bool
	}
	for _, family := range []struct {
		name  string
		new   func() resource.Resource
		model any
		cases []validationCase
	}{
		{"comment", newCommentResource, schemaComment(), []validationCase{
			{"invalid target unknown text", map[string]attr.Value{"object_type": types.StringValue("TABLE"), "text": types.StringUnknown()}, true},
			{"unknown schema", map[string]attr.Value{"object_type": types.StringValue("TABLE"), "schema_name": types.StringUnknown()}, false},
			{"unknown kind", map[string]attr.Value{"object_type": types.StringUnknown()}, false},
			{"database mismatch unknown text", map[string]attr.Value{"object_type": types.StringValue("DATABASE"), "text": types.StringUnknown()}, true},
			{"database unknown name", map[string]attr.Value{"object_type": types.StringValue("DATABASE"), "object_name": types.StringUnknown()}, false},
		}},
		{"view", newViewResource, testViewModel(), []validationCase{
			{"invalid query unknown name", map[string]attr.Value{"query": types.StringValue("SELECT 1; SELECT 2"), "name": types.StringUnknown()}, true},
			{"unknown query", map[string]attr.Value{"query": types.StringUnknown()}, false},
			{"empty schema unknown query", map[string]attr.Value{"schema": types.StringValue(""), "query": types.StringUnknown()}, true},
		}},
		{"materialized view", newMaterializedViewResource, testMaterializedViewModel(), []validationCase{
			{"invalid query unknown owner", map[string]attr.Value{"query": types.StringValue("SELECT 1; SELECT 2"), "owner": types.StringUnknown()}, true},
			{"unknown query", map[string]attr.Value{"query": types.StringUnknown()}, false},
			{"dependent unknown distribution key", map[string]attr.Value{"distribution": types.ObjectValueMust(materializedViewDistributionTypes, map[string]attr.Value{"style": types.StringValue("KEY"), "key": types.StringUnknown()})}, false},
			{"invalid storage unknown query", map[string]attr.Value{"query": types.StringUnknown(), "distribution": materializedViewDistributionBlock("ALL", "label")}, true},
		}},
		{"external function", newExternalFunctionResource, externalFunctionTestModel(), []validationCase{
			{"invalid return unknown role", map[string]attr.Value{"return_type": types.StringValue("super"), "iam_role": types.StringUnknown()}, true},
			{"unknown return", map[string]attr.Value{"return_type": types.StringUnknown()}, false},
			{"unknown argument", map[string]attr.Value{"arguments": types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})}, false},
			{"invalid known argument", map[string]attr.Value{"arguments": types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown(), types.StringValue("super")})}, true},
			{"unit unknown size", map[string]attr.Value{"max_batch_size": types.Int64Unknown(), "max_batch_size_unit": types.StringValue("MB")}, false},
			{"size unknown unit", map[string]attr.Value{"max_batch_size": types.Int64Value(6), "max_batch_size_unit": types.StringUnknown()}, false},
			{"invalid size unknown unit", map[string]attr.Value{"max_batch_size": types.Int64Value(6000), "max_batch_size_unit": types.StringUnknown()}, true},
		}},
		{"external partition", newExternalPartitionResource, externalPartitionTestModel(), []validationCase{
			{"invalid keys unknown location", map[string]attr.Value{"values": externalTableTestMap("day", "a", "DAY", "b"), "location": types.StringUnknown()}, true},
			{"unknown values", map[string]attr.Value{"values": types.MapUnknown(types.StringType)}, false},
			{"unknown value known duplicate keys", map[string]attr.Value{"values": types.MapValueMust(types.StringType, map[string]attr.Value{"day": types.StringUnknown(), "DAY": types.StringUnknown()})}, true},
			{"unknown value", map[string]attr.Value{"values": types.MapValueMust(types.StringType, map[string]attr.Value{"day": types.StringUnknown()})}, false},
		}},
		{"user", newUserResource, grafanaUser(), []validationCase{
			{"invalid limit unknown name", map[string]attr.Value{"connection_limit": types.Int64Value(-2), "name": types.StringUnknown()}, true},
			{"unknown disabled external id", map[string]attr.Value{"password_disabled": types.BoolUnknown(), "external_id": types.StringValue("abc")}, false},
			{"unknown password disabled", map[string]attr.Value{"password_wo": types.StringUnknown(), "password_disabled": types.BoolValue(true)}, false},
			{"invalid session key unknown value", map[string]attr.Value{"session_defaults": types.MapValueMust(types.StringType, map[string]attr.Value{"not_a_parameter": types.StringUnknown()})}, true},
		}},
		{"table security", newTableSecurityResource, tableSecurityLifecycleModel(), []validationCase{
			{"invalid conjunction unknown schema", map[string]attr.Value{"conjunction_type": types.StringValue("XOR"), "schema": types.StringUnknown()}, true},
			{"unknown schema", map[string]attr.Value{"schema": types.StringUnknown()}, false},
		}},
		{"rls attachment", newRlsPolicyAttachmentResource, rlsPolicyAttachmentLifecycleModel(), []validationCase{
			{"invalid public unknown policy", map[string]attr.Value{"grantee_type": types.StringValue("PUBLIC"), "policy": types.StringUnknown()}, true},
			{"public unknown grantee", map[string]attr.Value{"grantee_type": types.StringValue("PUBLIC"), "grantee": types.StringUnknown()}, false},
		}},
		{"masking attachment", newMaskingPolicyAttachmentResource, maskingTestAttachment(nil), []validationCase{
			{"invalid public unknown policy", map[string]attr.Value{"grantee_type": types.StringValue("PUBLIC"), "policy": types.StringUnknown()}, true},
			{"public unknown grantee", map[string]attr.Value{"grantee_type": types.StringValue("PUBLIC"), "grantee": types.StringUnknown()}, false},
			{"unknown column", map[string]attr.Value{"columns": types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()})}, false},
			{"duplicate columns unknown element", map[string]attr.Value{"columns": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("email"), types.StringUnknown(), types.StringValue("EMAIL")})}, true},
		}},
	} {
		t.Run(family.name, func(t *testing.T) {
			for _, test := range family.cases {
				t.Run(test.name, func(t *testing.T) {
					r := family.new()

					state := testState(t, r, family.model)
					for name, value := range test.values {
						require.False(t, state.SetAttribute(context.Background(), path.Root(name), value).HasError())
					}

					var response resource.ValidateConfigResponse
					r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
					assert.Equal(t, test.invalid, response.Diagnostics.HasError(), "%v", response.Diagnostics)
				})
			}
		})
	}
}
