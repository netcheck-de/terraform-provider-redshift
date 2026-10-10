package provider

import (
	"context"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_datashare_privilege", map[string]replaceRule{
	"database_name":  replaceAlways,
	"datashare_name": replaceAlways,
	"grantee":        replaceAlways,
	"grantee_type":   replaceAlways,
	"privileges":     replaceNever,
})

// datashareFullFields names the share, database, and role that fullCatalog contains.
var datashareFullFields = map[string]string{"database_name": "admin", "datashare_name": "producer", "grantee_type": "ROLE", "grantee": "example:readers"}

// TestDatasharePrivilegeLifecycle runs the shared permission lifecycle checks for every grantee form.
func TestDatasharePrivilegeLifecycle(t *testing.T) {
	for _, grantee := range []map[string]string{
		{"grantee_type": "ROLE", "grantee": "share_admins"},
		{"grantee_type": "USER", "grantee": "loader"},
		{"grantee_type": "GROUP", "grantee": "readers"},
		{"grantee_type": "PUBLIC", "grantee": "public"},
	} {
		t.Run(grantee["grantee_type"], func(t *testing.T) {
			fields := maps.Clone(datasharePrivilegeFields)
			maps.Copy(fields, grantee)
			exercisePrivilege(t, newDatasharePrivilegeResource, fields, []string{"ALTER", "SHARE"})
		})
	}
}

// TestDatasharePrivilegeRejectsGrantOptions keeps grant options unmanaged, because no documented REVOKE removes only
// the option on a datashare and a plain REVOKE would silently drop it.
func TestDatasharePrivilegeRejectsGrantOptions(t *testing.T) {
	r := newDatasharePrivilegeResource().(*privilegeResource)
	var response resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &response)
	assert.NotContains(t, response.Schema.Attributes, "grant_option_privileges")
	c := fullCatalog()
	fakeState[*fakeDatashares](c, "datashare").adminOption = true
	r.resourceClient = testResourceClient(c)
	data := privilegeObject(t, r, datashareFullFields, "ALTER")
	_, _, err := r.read(context.Background(), &data)
	require.ErrorContains(t, err, "grant options are not managed")
}

// TestDatasharePrivilegeCatalog runs the resource against the stateful fake: the producer database routes every
// statement, a dropped share removes the tuple from state, and deletion revokes only this grantee's permissions.
func TestDatasharePrivilegeCatalog(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	routes := map[string]string{}
	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		routes[sql] = connection.Database
		return c.Query(ctx, connection, sql, parameters)
	})
	fields := maps.Clone(datashareFullFields)
	fields["database_name"], fields["grantee_type"], fields["grantee"] = "warehouse", "PUBLIC", "public"
	r := newDatasharePrivilegeResource().(*privilegeResource)
	r.resourceClient = testResourceClient(client)
	data := privilegeObject(t, r, fields, "ALTER", "SHARE")
	require.NoError(t, r.reconcile(context.Background(), data))
	family := fakeState[*fakeDatashares](c, "datashare")
	assert.Equal(t, map[string]bool{"ALTER": true, "SHARE": true}, family.privileges["public"])
	assert.Equal(t, map[string]bool{"ALTER": true}, family.privileges["role:example:readers"], "other grantees stay untouched")
	assert.Equal(t, "warehouse", routes[`GRANT SHARE ON DATASHARE "producer" TO PUBLIC`], "mutations run in the producer database")
	assert.Equal(t, "warehouse", routes["SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = 'public'"])
	assert.Equal(t, "admin", routes["SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database"])
	require.False(t, invoke(t, r, "delete", data, false).HasError())
	assert.Empty(t, family.privileges["public"])
	assert.Equal(t, map[string]bool{"ALTER": true}, family.privileges["role:example:readers"])

	c.share = false
	state := testState(t, r, privilegeObject(t, r, datashareFullFields, "ALTER"))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.True(t, resp.State.Raw.IsNull(), "a dropped share removes the permission from state")
}

// TestDatasharePrivilegeValidation rejects invalid tuples at plan time and before Create records state.
func TestDatasharePrivilegeValidation(t *testing.T) {
	for name, test := range map[string]struct {
		changes    map[string]string
		privileges []string
	}{
		"public name":     {map[string]string{"grantee_type": "PUBLIC", "grantee": "everyone"}, []string{"SHARE"}},
		"empty share":     {map[string]string{"datashare_name": ""}, []string{"ALTER"}},
		"empty database":  {map[string]string{"database_name": ""}, []string{"ALTER"}},
		"unknown kind":    {map[string]string{"grantee_type": "NAMESPACE"}, []string{"ALTER"}},
		"usage privilege": {nil, []string{"USAGE"}},
		"all privileges":  {nil, []string{"ALL"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newDatasharePrivilegeResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				t.Fatalf("invalid tuple reached SQL: %s", sql)
				return nil, nil
			}))
			fields := maps.Clone(datasharePrivilegeFields)
			maps.Copy(fields, test.changes)
			data := privilegeObject(t, r, fields, test.privileges...)
			var schema resource.SchemaResponse
			r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
			plan := tfsdk.Plan{Schema: schema.Schema}
			require.False(t, plan.Set(context.Background(), data).HasError())
			resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
			r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			assert.True(t, resp.State.Raw.IsNull(), "invalid tuple must not be recorded in state")
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(plan)}, &validated)
			assert.True(t, validated.Diagnostics.HasError(), "invalid tuple must be reported during planning")
		})
	}
	r := newDatasharePrivilegeResource().(*privilegeResource)
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	unknown := privilegeObject(t, r, datasharePrivilegeFields, "ALTER")
	attributes := unknown.Attributes()
	attributes["grantee"] = types.StringUnknown()
	config := tfsdk.Config{Schema: schema.Schema}
	plan := tfsdk.Plan(config)
	require.False(t, plan.Set(context.Background(), types.ObjectValueMust(unknown.AttributeTypes(context.Background()), attributes)).HasError())
	var validated resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(plan)}, &validated)
	assert.False(t, validated.Diagnostics.HasError(), "unknown values defer validation: %v", validated.Diagnostics)
}

// TestDatasharePrivilegeTranscripts records the SQL conversation of every operation against the stateful fake.
func TestDatasharePrivilegeTranscripts(t *testing.T) {
	r := newDatasharePrivilegeResource().(*privilegeResource)
	object := func(fields map[string]string, privileges ...string) types.Object {
		return privilegeObject(t, r, fields, privileges...)
	}
	public := maps.Clone(datashareFullFields)
	public["grantee_type"], public["grantee"] = "PUBLIC", "public"
	group := maps.Clone(datashareFullFields)
	group["grantee_type"], group["grantee"] = "GROUP", "readers"
	runTranscripts(t, "privilege/datashare_privilege", newDatasharePrivilegeResource, []transcriptCase{
		{name: "create", operation: "create", catalog: catalogWith(), planned: object(datashareFullFields, "ALTER", "SHARE")},
		{name: "read", operation: "read", catalog: catalogWith(), prior: object(datashareFullFields, "ALTER")},
		{name: "update_replace", operation: "update", catalog: catalogWith(), prior: object(datashareFullFields, "ALTER"), planned: object(datashareFullFields, "SHARE")},
		{name: "update_unchanged", operation: "update", catalog: catalogWith(), prior: object(datashareFullFields, "ALTER"), planned: object(datashareFullFields, "ALTER")},
		{name: "delete", operation: "delete", catalog: catalogWith(), prior: object(datashareFullFields, "ALTER")},
		{name: "import", operation: "import", catalog: catalogWith(), planned: object(datashareFullFields, "ALTER")},
		{name: "create_public", operation: "create", catalog: catalogWith(), planned: object(public, "SHARE")},
		{name: "create_group", operation: "create", catalog: catalogWith(), planned: object(group, "ALTER")},
	})
}
