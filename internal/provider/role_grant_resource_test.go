package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "membership", kind: lifecyclePermission, new: newRoleGrantResource, model: roleGrantModel{Role: types.StringValue("sys:dba"), ToRole: types.StringValue("example:readers"), ToUser: types.StringNull(), AdminOption: types.BoolValue(false)}, absent: func(c *catalog) { c.membership = false }})

var _ = registerReplacementPolicy("redshift_role_grant", map[string]replaceRule{
	"role":         replaceAlways,
	"to_role":      replaceAlways,
	"to_user":      replaceAlways,
	"admin_option": replaceNever,
})

var _ = registerValidateConfigCase("role_grant", validateConfigCase{
	new:     newRoleGrantResource,
	valid:   roleGrantFixture("sys:monitor", "", "grafana", true),
	invalid: roleGrantFixture("sys:dba", "example:readers", "", true),
	unknown: roleGrantModel{Role: types.StringValue("sys:dba"), ToRole: types.StringValue("example:readers"), ToUser: types.StringNull(), AdminOption: types.BoolUnknown()},
})

// roleGrantUserCatalog is a fake where grafana exists and holds the granted role, with or without the admin option.
func roleGrantUserCatalog(granted, admin bool) *catalog {
	c := &catalog{user: true, userGrant: granted}
	fakeState[*roleGrantFakeFamily](c, "role_grant").admin = admin
	return c
}

// TestRoleGrantObservesSystemRole checks a built-in role granted to another role, which never has an admin option.
func TestRoleGrantObservesSystemRole(t *testing.T) {
	c := &catalog{role: true, membership: true}
	r := &roleGrantResource{testResourceClient(c)}
	data := roleGrantFixture("sys:dba", "example:readers", "", false)
	data.AdminOption = types.BoolNull()
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, types.BoolValue(false), data.AdminOption)
	c.membership = false
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRoleGrantObservesUserMembership checks role-to-user catalog membership and its admin option.
func TestRoleGrantObservesUserMembership(t *testing.T) {
	c := roleGrantUserCatalog(true, true)
	r := &roleGrantResource{testResourceClient(c)}
	data := roleGrantFixture("sys:monitor", "", "grafana", false)
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, data.AdminOption.ValueBool())
	c.userGrant = false
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestRoleGrantAdminOptionFromAnyGrantor treats one grantor's admin option as enough and rejects malformed flags.
func TestRoleGrantAdminOptionFromAnyGrantor(t *testing.T) {
	for _, test := range []struct {
		name    string
		options []string
		admin   bool
		fails   bool
	}{
		{"one of two grantors", []string{"false", "true"}, true, false},
		{"no grantor", []string{"f"}, false, false},
		{"malformed", []string{"maybe"}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &roleGrantResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				var rows []sqlclient.Row
				for _, option := range test.options {
					rows = append(rows, sqlclient.Row{"role_name": "sys:monitor", "admin_option": option})
				}
				return rows, nil
			}))}
			data := roleGrantFixture("sys:monitor", "", "grafana", false)
			_, err := r.read(context.Background(), &data)
			if test.fails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.admin, data.AdminOption.ValueBool())
		})
	}
}

// TestRoleGrantAdminOptionRequiresUser rejects WITH ADMIN OPTION for a role recipient before any SQL runs.
func TestRoleGrantAdminOptionRequiresUser(t *testing.T) {
	c := &catalog{role: true}
	r := newRoleGrantResource()
	configureTestResource(t, r, c)
	invalid := roleGrantFixture("sys:dba", "example:readers", "", true)
	diagnostics := invoke(t, r, "create", invalid, false)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Invalid role grant", diagnostics.Errors()[0].Summary())
	prior := roleGrantFixture("sys:dba", "example:readers", "", false)
	_, diagnostics = applyOperation(t, r, "update", prior, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestRoleGrantAdminOptionLifecycle grants, upgrades, downgrades, and verifies the admin option, including refused
// and unconverged statements.
func TestRoleGrantAdminOptionLifecycle(t *testing.T) {
	user, admin := roleGrantFixture("sys:monitor", "", "grafana", false), roleGrantFixture("sys:monitor", "", "grafana", true)
	for _, test := range []struct {
		name        string
		operation   string
		catalog     *catalog
		prior, plan roleGrantModel
		respond     func(sql string) (bool, error)
		summary     string
	}{
		{"create with option", "create", roleGrantUserCatalog(false, false), user, admin, nil, ""},
		{"upgrade", "update", roleGrantUserCatalog(true, false), user, admin, nil, ""},
		{"downgrade", "update", roleGrantUserCatalog(true, true), admin, user, nil, ""},
		{"refused downgrade", "update", roleGrantUserCatalog(true, true), admin, user, func(sql string) (bool, error) {
			return strings.HasPrefix(sql, "REVOKE ADMIN OPTION"), errors.New("REVOKE denied")
		}, "Update Redshift role admin option"},
		{"ignored upgrade", "update", roleGrantUserCatalog(true, false), user, admin, func(sql string) (bool, error) {
			return strings.HasPrefix(sql, "GRANT"), nil
		}, "Verify Redshift role membership"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if test.respond != nil {
					if handled, err := test.respond(sql); handled {
						return nil, err
					}
				}
				return test.catalog.Query(ctx, target, sql, parameters)
			})
			r := newRoleGrantResource()
			configureTestResource(t, r, client)
			_, diagnostics := applyOperation(t, r, test.operation, test.prior, test.plan, nil)
			if test.summary != "" {
				require.True(t, diagnostics.HasError())
				assert.Equal(t, test.summary, diagnostics.Errors()[0].Summary())
				return
			}
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.plan.adminOption(), fakeState[*roleGrantFakeFamily](test.catalog, "role_grant").admin)
		})
	}
}

// TestRoleGrantCreateVerifiesAdminOption fails when the catalog does not report the requested admin option.
func TestRoleGrantCreateVerifiesAdminOption(t *testing.T) {
	c := roleGrantUserCatalog(false, false)
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		// Grant membership without the option, as a server ignoring the clause would.
		return c.Query(ctx, target, strings.TrimSuffix(sql, " WITH ADMIN OPTION"), parameters)
	})
	r := newRoleGrantResource()
	configureTestResource(t, r, client)
	_, diagnostics := applyOperation(t, r, "create", nil, roleGrantFixture("sys:monitor", "", "grafana", true), nil)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Verify Redshift role membership", diagnostics.Errors()[0].Summary())
}

// TestRoleGrantAlterCoverage checks that the admin option, the only in-place attribute, has an alter step.
func TestRoleGrantAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newRoleGrantResource(), roleGrantAlterSteps)
}

// TestRoleGrantTranscripts records the admin option flows, which the representative role-to-role case cannot reach.
func TestRoleGrantTranscripts(t *testing.T) {
	user, admin := roleGrantFixture("sys:monitor", "", "grafana", false), roleGrantFixture("sys:monitor", "", "grafana", true)
	existing := func(granted, option bool) func() sqlclient.Client {
		return func() sqlclient.Client { return roleGrantUserCatalog(granted, option) }
	}
	runTranscripts(t, "lifecycle/role_grant_admin", newRoleGrantResource, []transcriptCase{
		{name: "create", operation: "create", catalog: existing(false, false), planned: admin},
		{name: "read", operation: "read", catalog: existing(true, true), prior: admin},
		{name: "upgrade", operation: "update", catalog: existing(true, false), prior: user, planned: admin},
		{name: "downgrade", operation: "update", catalog: existing(true, true), prior: admin, planned: user},
		{name: "delete", operation: "delete", catalog: existing(true, true), prior: admin},
		{name: "import", operation: "import", catalog: existing(false, false), planned: admin},
	})
}

// TestRoleGrantRejectsAmbiguousImport rejects missing or competing recipient identities.
func TestRoleGrantRejectsAmbiguousImport(t *testing.T) {
	r := &roleGrantResource{}
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	for _, id := range []string{"invalid", `{"workgroup_name":"warehouse","database":"admin","role":"sys:monitor"}`, `{"workgroup_name":"warehouse","database":"admin","role":"sys:monitor","to_user":"grafana","to_role":"readers"}`} {
		resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.True(t, resp.Diagnostics.HasError())
	}
}
