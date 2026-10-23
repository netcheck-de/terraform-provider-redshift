package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrivilegePartialKnownValidation reports known invalid object grant fields at plan time while others are unknown,
// and defers checks that need an unknown value.
func TestPrivilegePartialKnownValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		changes map[string]attr.Value
		invalid bool
	}{
		{"invalid privilege unknown database", map[string]attr.Value{"database_name": types.StringUnknown(), "privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("EXECUTE")})}, true},
		{"missing schema unknown grantee", map[string]attr.Value{"grantee": types.StringUnknown(), "schema_name": types.StringNull()}, true},
		{"unknown schema", map[string]attr.Value{"schema_name": types.StringUnknown()}, false},
		{"unknown object type", map[string]attr.Value{"object_type": types.StringUnknown()}, false},
		{"invalid signature unknown database", map[string]attr.Value{"database_name": types.StringUnknown(), "object_type": types.StringValue("FUNCTION"), "arguments": types.StringValue("invalid_type")}, true},
		{"unknown privileges with option", map[string]attr.Value{"privileges": types.SetUnknown(types.StringType), "grant_option_privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, false},
		{"partially known privileges with option", map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringUnknown()}), "grant_option_privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, false},
		{"known invalid privilege among unknown", map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringUnknown(), types.StringValue("EXECUTE")})}, true},
		{"known invalid option recipient", map[string]attr.Value{"database_name": types.StringUnknown(), "grantee_type": types.StringValue("ROLE"), "grant_option_privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			data := privilegeObject(t, r, map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "object_type": "TABLE", "grantee": "reader", "grantee_type": "USER"}, "SELECT")
			for key, value := range test.changes {
				lookupValue(&data, key, value)
			}
			state := testState(t, r, data)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
			assert.Equal(t, test.invalid, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		})
	}
}

// TestGrantPartialKnownValidation rejects a privilege invalid for a known scope, and defers the check while scope,
// schema, grantee, or privileges are unknown.
func TestGrantPartialKnownValidation(t *testing.T) {
	data := grantModel{DatabaseName: types.StringUnknown(), Scope: types.StringValue("TABLES"), Role: types.StringValue("reader"), Privileges: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})}
	require.ErrorContains(t, data.validate(), "not \"USAGE\"")
	data.Scope = types.StringUnknown()
	require.NoError(t, data.validate())
	data.Scope, data.SchemaName = types.StringValue("SCHEMA"), types.StringUnknown()
	require.NoError(t, data.validate())
	data.Role, data.User = types.StringNull(), types.StringUnknown()
	data.Privileges = types.SetUnknown(types.StringType)
	data.GrantOptionPrivileges = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})
	require.NoError(t, data.validate())
}

// TestColumnGrantPartialKnownValidation rejects an unsupported column privilege even when its columns and grantee are
// unknown.
func TestColumnGrantPartialKnownValidation(t *testing.T) {
	data := columnGrantModel{DatabaseName: types.StringUnknown(), Grantee: types.StringUnknown(), GranteeType: types.StringValue("USER"), Privileges: types.MapValueMust(types.SetType{ElemType: types.StringType}, map[string]attr.Value{"DELETE": types.SetUnknown(types.StringType)})}
	require.ErrorContains(t, validateColumnGrant(data), "unsupported column privilege")
	data.Privileges = types.MapValueMust(types.SetType{ElemType: types.StringType}, map[string]attr.Value{"SELECT": types.SetUnknown(types.StringType)})
	require.NoError(t, validateColumnGrant(data))
}

// TestDebugGrantPartialKnownValidation rejects a known local grantee, which cannot receive DEBUG, while the database is
// unknown, and defers the check while the grantee is unknown.
func TestDebugGrantPartialKnownValidation(t *testing.T) {
	r := newDebugGrantResource().(*debugGrantResource)
	data := debugGrantModel{Database: types.StringUnknown(), Grantee: types.StringValue("local_user"), GranteeType: types.StringValue("USER")}
	state := testState(t, r, data)
	var response resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
	assert.True(t, response.Diagnostics.HasError())
	data.Grantee = types.StringUnknown()
	state = testState(t, r, data)
	response = resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
	assert.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
}
