package provider

import (
	"context"
	"fmt"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attributeReplacement invokes the actual schema modifiers with a changed or unchanged input.
func attributeReplacement(t *testing.T, attribute schema.Attribute, changed, existing bool) bool {
	t.Helper()
	ctx := context.Background()
	var before, after attr.Value
	switch attribute.(type) {
	case schema.StringAttribute:
		before, after = types.StringValue("before"), types.StringValue("after")
	case schema.BoolAttribute:
		before, after = types.BoolValue(false), types.BoolValue(true)
	case schema.Int64Attribute:
		before, after = types.Int64Value(0), types.Int64Value(1)
	case schema.SetAttribute:
		before = types.SetValueMust(types.StringType, nil)
		after = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})
	default:
		t.Fatalf("add replacement coverage for attribute type %T", attribute)
	}
	if !changed {
		after = before
	}
	attributeType := attribute.GetType().TerraformType(ctx)
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"value": attributeType}}
	beforeValue, err := before.ToTerraformValue(ctx)
	require.NoError(t, err)
	afterValue, err := after.ToTerraformValue(ctx)
	require.NoError(t, err)
	single := schema.Schema{Attributes: map[string]schema.Attribute{"value": attribute}}
	state := tfsdk.State{Schema: single, Raw: tftypes.NewValue(objectType, map[string]tftypes.Value{"value": beforeValue})}
	if !existing {
		state.Raw = tftypes.NewValue(objectType, nil)
	}
	plan := tfsdk.Plan{Schema: single, Raw: tftypes.NewValue(objectType, map[string]tftypes.Value{"value": afterValue})}
	config := tfsdk.Config{Schema: single, Raw: plan.Raw}
	replace := false
	switch attribute := attribute.(type) {
	case schema.StringAttribute:
		request := planmodifier.StringRequest{Path: path.Root("value"), Config: config, Plan: plan, State: state, ConfigValue: after.(types.String), PlanValue: after.(types.String), StateValue: before.(types.String)}
		for _, modifier := range attribute.PlanModifiers {
			response := planmodifier.StringResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyString(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	case schema.BoolAttribute:
		request := planmodifier.BoolRequest{Path: path.Root("value"), Config: config, Plan: plan, State: state, ConfigValue: after.(types.Bool), PlanValue: after.(types.Bool), StateValue: before.(types.Bool)}
		for _, modifier := range attribute.PlanModifiers {
			response := planmodifier.BoolResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyBool(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	case schema.Int64Attribute:
		request := planmodifier.Int64Request{Path: path.Root("value"), Config: config, Plan: plan, State: state, ConfigValue: after.(types.Int64), PlanValue: after.(types.Int64), StateValue: before.(types.Int64)}
		for _, modifier := range attribute.PlanModifiers {
			response := planmodifier.Int64Response{PlanValue: request.PlanValue}
			modifier.PlanModifyInt64(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	case schema.SetAttribute:
		request := planmodifier.SetRequest{Path: path.Root("value"), Config: config, Plan: plan, State: state, ConfigValue: after.(types.Set), PlanValue: after.(types.Set), StateValue: before.(types.Set)}
		for _, modifier := range attribute.PlanModifiers {
			response := planmodifier.SetResponse{PlanValue: request.PlanValue}
			modifier.PlanModifySet(ctx, request, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			replace = replace || response.RequiresReplace
		}
	}
	return replace
}

// TestEveryResourceAttributeReplacementPolicy covers every input and computed attribute across all resources.
func TestEveryResourceAttributeReplacementPolicy(t *testing.T) {
	policies := map[string]map[string]bool{
		"redshift_database":           {"name": true, "datashare_arn": true, "with_permissions": true},
		"redshift_datashare":          {"database": true, "name": true, "publicly_accessible": false},
		"redshift_schema":             {"database": true, "name": true},
		"redshift_external_schema":    {"database": true, "name": true, "glue_database": true, "iam_role_arn": true, "region": true, "refresh_revision": true},
		"redshift_identity_provider":  {"name": true, "namespace": true, "application_arn": true, "iam_role_arn": false, "enabled": false},
		"redshift_role":               {"name": true},
		"redshift_user":               {"name": true, "superuser": false, "create_database": false, "password_wo": false, "password_wo_version": false},
		"redshift_group":              {"name": true},
		"redshift_role_grant":         {"role": true, "to_role": true, "to_user": true},
		"redshift_group_membership":   {"group": true, "user": true},
		"redshift_datashare_grant":    {"database": true, "datashare": true, "account_id": true, "namespace_id": true},
		"redshift_datashare_schema":   {"database": true, "datashare": true, "schema": true, "include_new": false},
		"redshift_datashare_table":    {"database": true, "datashare": true, "schema": true, "table": true},
		"redshift_grant":              {"database_name": true, "schema_name": true, "role": true, "datashare": true, "scope": true, "privileges": false},
		"redshift_object_grant":       {"database_name": true, "schema_name": true, "object_name": true, "object_type": true, "grantee": true, "grantee_type": true, "privileges": false},
		"redshift_system_grant":       {"role": true, "privileges": false},
		"redshift_assumerole_grant":   {"iam_role_arn": true, "grantee": true, "grantee_type": true, "privileges": false},
		"redshift_default_privileges": {"database_name": true, "schema_name": true, "owner": true, "object_type": true, "grantee": true, "grantee_type": true, "privileges": false},
		"redshift_comment":            {"database_name": true, "schema_name": true, "object_type": true, "object_name": true, "column_name": true, "text": false},
	}
	factories := New("test")().Resources(context.Background())
	require.Len(t, policies, len(factories))
	for _, factory := range factories {
		instance := factory()
		var metadata resource.MetadataResponse
		instance.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		t.Run(metadata.TypeName, func(t *testing.T) {
			policy, found := policies[metadata.TypeName]
			require.True(t, found, "resource needs an explicit replacement policy")
			var response resource.SchemaResponse
			instance.Schema(context.Background(), resource.SchemaRequest{}, &response)
			inputs := 0
			for name, attribute := range response.Schema.Attributes {
				t.Run(name, func(t *testing.T) {
					expected := false
					if attribute.IsRequired() || attribute.IsOptional() {
						inputs++
						var found bool
						expected, found = policy[name]
						require.True(t, found, "input needs an explicit replacement policy")
					}
					assert.Equal(t, expected, attributeReplacement(t, attribute, true, true), "changed input")
					assert.False(t, attributeReplacement(t, attribute, false, true), "unchanged input")
					assert.False(t, attributeReplacement(t, attribute, true, false), "initial creation")
				})
			}
			assert.Len(t, policy, inputs, "policy must not contain stale attribute names")
		})
	}
}

// TestReferencedRoleNameChangeReplacesMembership verifies Terraform propagates names without caller lifecycle triggers.
func TestReferencedRoleNameChangeReplacesMembership(t *testing.T) {
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(name string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_role_grant" "reader" {
  role = "sys:dba"
  to_role = redshift_role.reader.name
}`, name)
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("before")},
			{Config: configuration("after"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_role.reader", plancheck.ResourceActionDestroyBeforeCreate),
				plancheck.ExpectResourceAction("redshift_role_grant.reader", plancheck.ResourceActionDestroyBeforeCreate),
			}}, Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_role.reader", "name", "after"),
				testresource.TestCheckResourceAttr("redshift_role_grant.reader", "to_role", "after"),
			)},
			{Config: configuration("after"), PlanOnly: true},
		},
	})
}
