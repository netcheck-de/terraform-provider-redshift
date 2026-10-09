package provider

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "identity", new: newIdentityProviderResource, model: identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}, absent: func(c *catalog) { c.identity = false }, dependents: func(c *catalog) { c.role = false }})

var _ = registerReplacementPolicy("redshift_identity_provider", map[string]replaceRule{
	"name":            replaceAlways,
	"namespace":       replaceAlways,
	"application_arn": replaceAlways,
	"iam_role_arn":    replaceNever,
	"enabled":         replaceNever,
})

// TestIdentityProviderObservesMutableAttributes checks integration role and enabled-state refresh.
func TestIdentityProviderObservesMutableAttributes(t *testing.T) {
	c := &catalog{identity: true, iamRole: "role-one", enabled: true}
	r := &identityProviderResource{testResourceClient(c)}
	data := identityProviderModel{Name: types.StringValue("identity")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "role-one", data.IAMRoleARN.ValueString())
	assert.True(t, data.Enabled.ValueBool())
	c.iamRole, c.enabled = "role-two", false
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "role-two", data.IAMRoleARN.ValueString())
	assert.False(t, data.Enabled.ValueBool())
}

// TestIdentityProviderKeepsFederatedUsers prevents deleting integrations with unmanaged users.
func TestIdentityProviderKeepsFederatedUsers(t *testing.T) {
	writes := 0
	r := &identityProviderResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT name"):
			return []dataapi.Row{{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role"}`, "enabled": "true"}}, nil
		case strings.HasPrefix(sql, "SELECT usename"):
			return []dataapi.Row{{"usename": "example:user"}}, nil
		default:
			writes++
			return nil, nil
		}
	}))}
	data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role"), Enabled: types.BoolValue(true)}
	diagnostics := invoke(t, r, "delete", data, false)
	assert.True(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Zero(t, writes, "federated provider must not be dropped")
}

// TestIdentityProviderRejectsInvalidCatalog checks AWSIDC catalog identity and option validation.
func TestIdentityProviderRejectsInvalidCatalog(t *testing.T) {
	for name, change := range map[string]map[string]string{
		"provider type": {"type": "azure"},
		"parameters":    {"params": "invalid JSON"},
		"IAM role":      {"params": "{}"},
		"enabled flag":  {"enabled": "invalid"},
		"namespace":     {"namespc": ""},
		"application":   {"instanceid": ""},
	} {
		t.Run(name, func(t *testing.T) {
			row := dataapi.Row{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role-one"}`, "enabled": "true"}
			maps.Copy(row, change)
			r := &identityProviderResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return []dataapi.Row{row}, nil
			}))}
			data := identityProviderModel{Name: types.StringValue("identity")}
			_, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}

// TestIdentityProviderRejectsDuplicateRows prevents selecting an arbitrary integration binding.
func TestIdentityProviderRejectsDuplicateRows(t *testing.T) {
	row := dataapi.Row{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role-one"}`, "enabled": "true"}
	r := &identityProviderResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return []dataapi.Row{row, row}, nil
	}))}
	_, err := r.read(context.Background(), &identityProviderModel{Name: types.StringValue("identity")})
	require.ErrorContains(t, err, "ambiguous catalog metadata")
}

// TestIdentityProviderRefusesChangedIdentityOnDelete prevents cleanup of a rebound integration.
func TestIdentityProviderRefusesChangedIdentityOnDelete(t *testing.T) {
	for _, field := range []string{"namespace", "application"} {
		t.Run(field, func(t *testing.T) {
			c := &catalog{identity: true, enabled: true, iamRole: "role-one"}
			r := &identityProviderResource{testResourceClient(c)}
			data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}
			if field == "namespace" {
				data.Namespace = types.StringValue("previous")
			} else {
				data.ApplicationARN = types.StringValue("previous")
			}
			diagnostics := invoke(t, r, "delete", data, false)
			assert.True(t, diagnostics.HasError())
			assert.Empty(t, c.writes)
		})
	}
}

// TestIdentityProviderCreatesDisabled verifies an explicitly disabled integration is created correctly.
func TestIdentityProviderCreatesDisabled(t *testing.T) {
	for _, failAt := range []int{0, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			c := &catalog{}
			calls := 0
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				calls++
				if calls == failAt {
					return nil, fmt.Errorf("disable failed")
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &identityProviderResource{testResourceClient(client)}
			data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(false)}
			diagnostics := invoke(t, r, "create", data, false)
			assert.Equal(t, failAt != 0, diagnostics.HasError(), "%v", diagnostics)
			if failAt == 0 {
				assert.False(t, c.enabled)
			}
		})
	}
}

// TestIdentityProviderUpdateNamesFailedStatement keeps the role and status failures apart, so a refused update
// says which setting Redshift rejected.
func TestIdentityProviderUpdateNamesFailedStatement(t *testing.T) {
	for suffix, summary := range map[string]string{"IAM_ROLE 'role-two'": "Update identity provider role", `"identity" DISABLE`: "Update identity provider status"} {
		t.Run(summary, func(t *testing.T) {
			c := &catalog{identity: true, iamRole: "role-one", enabled: true}
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasSuffix(sql, suffix) {
					return nil, fmt.Errorf("ALTER denied")
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &identityProviderResource{testResourceClient(client)}
			data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-two"), Enabled: types.BoolValue(false)}
			data.ID = r.identity("admin", map[string]string{"name": "identity"})
			diagnostics := invoke(t, r, "update", data, false)
			require.True(t, diagnostics.HasError())
			assert.Equal(t, summary, diagnostics.Errors()[0].Summary())
		})
	}
}
