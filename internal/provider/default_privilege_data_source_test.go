package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDefaultPrivilegeDataSource, resource: newDefaultPrivilegeResource, selectors: []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}})

// TestDefaultPrivilegeLookup reads creator-specific defaults without affecting future object grants.
func TestDefaultPrivilegeLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDefaultPrivilegeDataSource, map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee": "readers", "grantee_type": "ROLE"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, &privilegeCatalog{values: map[string]bool{"SELECT": true}})
}

// TestDefaultPrivilegeLookupIdentitySelectors checks database-wide defaults and their ownership binding.
func TestDefaultPrivilegeLookupIdentitySelectors(t *testing.T) {
	fields := map[string]string{"database_name": "analytics", "owner": "loader", "object_type": "TABLES", "grantee": "readers", "grantee_type": "ROLE"}
	exerciseCatalogLookup(t, newDefaultPrivilegeDataSource, fields, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, nil)}, queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "FROM svv_default_privileges") {
			assert.Equal(t, "analytics", target.Database)
			assert.NotContains(t, parameters, "schema")

			return nil, nil
		}

		assert.Equal(t, "admin", target.Database)

		return []sqlclient.Row{{}}, nil
	}))
}
