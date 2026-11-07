package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_default_privilege", map[string]replaceRule{
	"restore_default_on_destroy": replaceNever,
	"database_name":              replaceAlways,
	"schema_name":                replaceAlways,
	"owner":                      replaceAlways,
	"object_type":                replaceAlways,
	"grantee":                    replaceAlways,
	"grantee_type":               replaceAlways,
	"privileges":                 replaceNever,
	"grant_option_privileges":    replaceNever,
})

// TestDefaultPrivilegeLifecycle exercises schema-specific creator default permissions.
func TestDefaultPrivilegeLifecycle(t *testing.T) {
	exercisePrivilege(t, newDefaultPrivilegeResource, map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee_type": "ROLE", "grantee": "readers"}, []string{"SELECT", "INSERT"})
}

// TestDefaultPrivilegeObjectTypes checks routine/table defaults and global parameter handling.
func TestDefaultPrivilegeObjectTypes(t *testing.T) {
	r := newDefaultPrivilegeResource().(*privilegeResource)
	for _, kind := range []string{"FUNCTIONS", "PROCEDURES", "TABLES", "invalid"} {
		target, err := r.prepare(privilegeObject(t, r, map[string]string{"database_name": "warehouse", "owner": "loader", "object_type": kind, "grantee_type": "ROLE", "grantee": "readers"}))
		if kind == "invalid" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.NotContains(t, target.query.parameters, "schema", "Data API parameters cannot contain empty strings")
		}
	}

	_, err := r.prepare(privilegeObject(t, r, map[string]string{"grantee_type": "invalid"}))
	require.Error(t, err)
}

// TestDefaultPrivilegeAllowlist lists the privileges each object type accepts and defers an unknown type.
func TestDefaultPrivilegeAllowlist(t *testing.T) {
	r := newDefaultPrivilegeResource().(*privilegeResource)

	for kind, count := range map[string]int{"TABLES": 7, "FUNCTIONS": 1, "PROCEDURES": 1, "VIEWS": 0} {
		t.Run(kind, func(t *testing.T) {
			data := privilegeObject(t, r, map[string]string{"object_type": kind})

			allowed, err := defaultPrivilegeConfigAllowed(data)
			if count == 0 {
				require.ErrorContains(t, err, "unsupported default object_type")
				return
			}

			require.NoError(t, err)
			assert.Len(t, allowed, count)
		})
	}

	data := privilegeObject(t, r, nil)
	attributes := data.Attributes()
	attributes["object_type"] = types.StringUnknown()
	allowed, err := defaultPrivilegeConfigAllowed(types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes))
	require.NoError(t, err)
	assert.Nil(t, allowed)
}

// TestDefaultPrivilegeMovesFromPreviousName lets a moved block transfer state recorded as redshift_default_privileges
// unchanged, while other sources stay unmatched so Terraform reports that nothing accepts them.
func TestDefaultPrivilegeMovesFromPreviousName(t *testing.T) {
	ctx := context.Background()
	movers := newDefaultPrivilegeResource().(resource.ResourceWithMoveState).MoveState(ctx)
	require.Len(t, movers, 1)

	stateSchema := *movers[0].SourceSchema
	source := tfsdk.State{Schema: stateSchema, Raw: tftypes.NewValue(stateSchema.Type().TerraformType(ctx), nil)}

	for name, value := range map[string]string{"id": `{"database_name":"warehouse"}`, "database_name": "warehouse", "owner": "loader", "object_type": "TABLES", "grantee": "readers", "grantee_type": "ROLE"} {
		require.False(t, source.SetAttribute(ctx, path.Root(name), value).HasError(), name)
	}

	for _, test := range []struct {
		name, provider, typeName string
		moved                    bool
	}{
		{"previous name", "registry.terraform.io/netcheck-de/redshift", "redshift_default_privileges", true},
		{"mirrored provider", "terraform.example.com/netcheck-de/redshift", "redshift_default_privileges", true},
		{"other type", "registry.terraform.io/netcheck-de/redshift", "redshift_grant", false},
		{"other provider", "registry.terraform.io/brainly/redshift", "redshift_default_privileges", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := resource.MoveStateResponse{TargetState: tfsdk.State{Schema: stateSchema, Raw: tftypes.NewValue(stateSchema.Type().TerraformType(ctx), nil)}}
			movers[0].StateMover(ctx, resource.MoveStateRequest{SourceProviderAddress: test.provider, SourceTypeName: test.typeName, SourceState: &source}, &response)
			require.False(t, response.Diagnostics.HasError())

			if test.moved {
				assert.True(t, response.TargetState.Raw.Equal(source.Raw))
			} else {
				assert.True(t, response.TargetState.Raw.IsNull())
			}
		})
	}
}
