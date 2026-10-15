package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "role", new: newRoleResource, model: roleModel{Name: types.StringValue("example:readers")}, absent: func(c *catalog) { c.role = false }})

var _ = registerReplacementPolicy("redshift_role", map[string]replaceRule{
	"name":        replaceAlways,
	"owner":       replaceNever,
	"external_id": replaceNever,
})

// roleWithOptions is a role whose owner and external ID are both managed.
func roleWithOptions(owner, external string) roleModel {
	return roleModel{Name: types.StringValue("example:readers"), Owner: types.StringValue(owner), ExternalID: types.StringValue(external), RoleID: types.Int64Null()}
}

// TestRoleReadsCatalogName checks the refresh of the role name, owner, external ID, and ID from svv_roles.
func TestRoleReadsCatalogName(t *testing.T) {
	c := &catalog{role: true}
	r := &roleResource{testResourceClient(c)}
	data := roleModel{Name: types.StringValue("example:readers")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "example:readers", data.Name.ValueString())
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.True(t, data.ExternalID.IsNull(), "a NULL external ID is null")
	assert.Equal(t, int64(100), data.RoleID.ValueInt64())
	state := fakeState[*roleFakeFamily](c, "role")
	state.owner, state.externalID = "loader", "ABC123"
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "loader", data.Owner.ValueString())
	assert.Equal(t, "ABC123", data.ExternalID.ValueString())
}

// TestRoleRejectsInvalidCatalog reports ambiguous rows and malformed IDs instead of adopting one of them.
func TestRoleRejectsInvalidCatalog(t *testing.T) {
	row := sqlclient.Row{"role_id": "100", "role_name": "readers", "role_owner": "admin", "external_id": ""}
	for name, rows := range map[string][]sqlclient.Row{
		"duplicate rows": {row, row},
		"role id":        {{"role_id": "x", "role_name": "readers", "role_owner": "admin"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &roleResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return rows, nil
			}))}
			_, err := r.read(context.Background(), &roleModel{Name: types.StringValue("readers")})
			require.Error(t, err)
		})
	}
	r := &roleResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"role_id": "", "role_name": "readers"}}, nil
	}))}
	data := roleModel{Name: types.StringValue("readers")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, data.RoleID.IsNull(), "a NULL role ID is null")
}

// TestRoleAlterCoverage checks that owner and external ID, the in-place attributes, each have an alter step.
func TestRoleAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newRoleResource(), roleAlterSteps)
}

// TestRoleCreateFailures injects failures after CREATE ROLE and checks that the identity is kept and reported.
func TestRoleCreateFailures(t *testing.T) {
	for name, fail := range map[string]func(sql string) bool{
		"owner transfer": func(sql string) bool { return strings.HasPrefix(sql, "ALTER ROLE") },
		"owner ignored":  func(string) bool { return false },
	} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{identity: true}
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if fail(sql) {
					return nil, errors.New("ALTER denied")
				}
				if name == "owner ignored" && strings.HasPrefix(sql, "ALTER ROLE") {
					return nil, nil // An acknowledged ownership change that did not converge.
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newRoleResource()
			configureTestResource(t, r, client)
			state, diagnostics := applyOperation(t, r, "create", nil, roleWithOptions("loader", "ABC123"), nil)
			require.True(t, diagnostics.HasError())
			assert.True(t, c.role, "the role was created before the failure")
			var id types.String
			require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
			assert.False(t, id.IsNull(), "the created role's identity is kept for the next plan")
		})
	}
}

// TestRoleUpdateFailures reports a refused ALTER ROLE and a change the catalog does not reflect.
func TestRoleUpdateFailures(t *testing.T) {
	prior := roleWithOptions("admin", "ABC123")
	planned := roleWithOptions("loader", "ABC123")
	for name, respond := range map[string]func() error{
		"refused":     func() error { return errors.New("ALTER denied") },
		"not applied": func() error { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{identity: true, role: true}
			fakeState[*roleFakeFamily](c, "role").externalID = "ABC123"
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "ALTER ROLE") {
					return nil, respond()
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newRoleResource()
			configureTestResource(t, r, client)
			prior.ID = r.(*roleResource).identity("admin", map[string]string{"name": "example:readers"})
			planned.ID = prior.ID
			_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
			require.True(t, diagnostics.HasError())
		})
	}
}

// TestRoleTranscripts records owner and external ID changes, which the representative lifecycle case leaves unset.
func TestRoleTranscripts(t *testing.T) {
	withOptions := roleWithOptions("loader", "ABC123")
	owned := roleWithOptions("admin", "ABC123")
	external := func(c *catalog) func() sqlclient.Client {
		return func() sqlclient.Client {
			c.identity, c.role = true, true
			fakeState[*roleFakeFamily](c, "role").externalID = "ABC123"
			return c
		}
	}
	runTranscripts(t, "lifecycle/role_options", newRoleResource, []transcriptCase{
		{name: "create_owner_external_id", operation: "create", catalog: catalogWith(func(c *catalog) { c.role = false }), planned: withOptions},
		{name: "update_owner", operation: "update", catalog: external(&catalog{}), prior: owned, planned: withOptions},
		{name: "update_external_id", operation: "update", catalog: external(&catalog{}), prior: owned, planned: roleWithOptions("admin", "XYZ456")},
		{name: "import", operation: "import", catalog: catalogWith(func(c *catalog) { c.role = false }), planned: withOptions},
	})
}
