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

var _ = registerParity(parityCase{source: newGrantDataSource, resource: newGrantResource, selectors: []string{"database_name", "schema_name", "role", "user", "datashare", "scope"}})

// TestScopedGrantLookup observes explicit scope permissions and errors on missing parents.
func TestScopedGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newGrantDataSource, map[string]string{"database_name": "analytics", "role": "example:readers", "scope": "TABLES"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, &catalog{database: true, role: true, privileges: map[string]bool{"SELECT": true}})
}

// TestScopedGrantLookupUserOptions observes a user's scoped privileges together with their grant options.
func TestScopedGrantLookupUserOptions(t *testing.T) {
	selectOnly := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})
	exerciseCatalogLookup(t, newGrantDataSource, map[string]string{"database_name": "analytics", "user": grantFakeUser, "scope": "TABLES"}, map[string]attr.Value{"privileges": selectOnly, "grant_option_privileges": selectOnly}, fullCatalog())
}

// TestScopedGrantLookupIdentitySelectors verifies optional identity keys and database routing.
func TestScopedGrantLookupIdentitySelectors(t *testing.T) {
	for _, fields := range []map[string]string{
		{"database_name": "analytics", "role": "readers", "scope": "TABLES"},
		{"database_name": "analytics", "role": "readers", "scope": "TABLES", "schema_name": "serving"},
		{"database_name": "analytics", "datashare": "producer", "scope": "TABLES", "schema_name": "serving"},
	} {
		t.Run(fields["role"]+fields["datashare"]+fields["schema_name"], func(t *testing.T) {
			exerciseCatalogLookup(t, newGrantDataSource, fields, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, nil)}, queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "SHOW ") {
					assert.Equal(t, "analytics", target.Database)
					return nil, nil
				}
				if parameters["share"] != "" {
					assert.Equal(t, "analytics", target.Database)
				} else {
					assert.Equal(t, "admin", target.Database)
				}
				return []sqlclient.Row{{"database_type": "local"}}, nil
			}))
		})
	}
}
