package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantRestoreDefaultOnDestroy(t *testing.T) {
	for _, test := range []struct {
		name                 string
		factory              func() resource.Resource
		fields               map[string]string
		privilege, statement string
	}{
		{"language", newLanguageGrantResource, map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "public", "grantee_type": "PUBLIC"}, "USAGE", `GRANT USAGE ON LANGUAGE SQL TO PUBLIC`},
		{"functions", newDefaultPrivilegesResource, map[string]string{"database_name": "warehouse", "owner": "loader", "object_type": "FUNCTIONS", "grantee": "public", "grantee_type": "PUBLIC"}, "EXECUTE", `ALTER DEFAULT PRIVILEGES FOR USER "loader" GRANT EXECUTE ON FUNCTIONS TO PUBLIC`},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Old states and imports have no cleanup policy until configuration is applied.
			for _, restore := range []types.Bool{types.BoolNull(), types.BoolValue(false), types.BoolValue(true)} {
				r := test.factory().(*privilegeResource)
				c := &privilegeCatalog{values: map[string]bool{}, grantee: "public", kind: "public"}
				r.resourceClient = testResourceClient(c)
				data := privilegeObject(t, r, test.fields)
				lookupValue(&data, "restore_default_on_destroy", restore)
				diagnostics := invoke(t, r, "delete", data)
				require.False(t, diagnostics.HasError(), "%v", diagnostics)
				assert.Equal(t, restore.ValueBool(), c.values[test.privilege])
				if restore.ValueBool() {
					assert.Equal(t, []string{test.statement}, c.writes)
				} else {
					assert.NotContains(t, c.writes, test.statement)
				}
			}
			r := test.factory().(*privilegeResource)
			r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{}, stuck: true})
			data := privilegeObject(t, r, test.fields)
			lookupValue(&data, "restore_default_on_destroy", types.BoolValue(true))
			assert.True(t, invoke(t, r, "delete", data).HasError(), "restoration must verify convergence")
			lookupValue(&data, "grantee_type", types.StringValue("ROLE"))
			require.ErrorContains(t, r.validate(data), "restore_default_on_destroy")
		})
	}
}

func TestDefaultPrivilegesRestoreScope(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	data := privilegeObject(t, r, map[string]string{"database_name": "warehouse", "object_type": "FUNCTIONS", "grantee": "public", "grantee_type": "PUBLIC"})
	lookupValue(&data, "restore_default_on_destroy", types.BoolValue(true))
	require.NoError(t, r.validate(data), "connecting user's defaults can also be restored")
	lookupValue(&data, "schema_name", types.StringValue("public"))
	require.ErrorContains(t, r.validate(data), "restore_default_on_destroy")
	lookupValue(&data, "schema_name", types.StringNull())
	lookupValue(&data, "object_type", types.StringValue("PROCEDURES"))
	require.ErrorContains(t, r.validate(data), "restore_default_on_destroy")
	// Cleanup policy is not a catalog observation and must not leak into lookup schemas.
	assert.NotContains(t, lookupAttributes(r.attributes, false, nil), "restore_default_on_destroy")
}

// TestDefaultPrivilegesRestoreRequiresDefaults rejects restore_default_on_destroy without restorable defaults, in validation and on destroy.
func TestDefaultPrivilegesRestoreRequiresDefaults(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{}, grantee: "reader", kind: "user"})
	data := privilegeObject(t, r, map[string]string{"database_name": "warehouse", "grantee": "reader", "grantee_type": "USER", "object_type": "TABLES"})
	attributes := data.Attributes()
	attributes["restore_default_on_destroy"] = types.BoolValue(true)
	data = types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes)
	require.ErrorContains(t, r.validateConfig(data), "restore_default_on_destroy requires")
	state := testState(t, r, data)
	var response resource.DeleteResponse
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &response)
	require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "restore_default_on_destroy requires")
	allowed, err := fixedPrivilegeAllowlist([]sqlclient.Keyword{"USAGE"})(data)
	require.NoError(t, err)
	assert.Equal(t, []sqlclient.Keyword{"USAGE"}, allowed)
}
