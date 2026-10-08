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

// TestDefaultPrivilegesLookup reads creator-specific defaults without affecting future object grants.
func TestDefaultPrivilegesLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDefaultPrivilegesDataSource, map[string]string{"database_name": "warehouse", "owner": "loader", "schema_name": "serving", "object_type": "TABLES", "grantee": "readers", "grantee_type": "ROLE"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, &privilegeCatalog{values: map[string]bool{"SELECT": true}})
}

// TestDefaultPrivilegesLookupIdentitySelectors checks database-wide defaults and their ownership binding.
func TestDefaultPrivilegesLookupIdentitySelectors(t *testing.T) {
	fields := map[string]string{"database_name": "analytics", "owner": "loader", "object_type": "TABLES", "grantee": "readers", "grantee_type": "ROLE"}
	exerciseCatalogLookup(t, newDefaultPrivilegesDataSource, fields, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, nil)}, queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "FROM svv_default_privileges") {
			assert.Equal(t, "analytics", target.Database)
			assert.NotContains(t, parameters, "schema")
			return nil, nil
		}
		assert.Equal(t, "admin", target.Database)
		return []sqlclient.Row{{}}, nil
	}))
}
