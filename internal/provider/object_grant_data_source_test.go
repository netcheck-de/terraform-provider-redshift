package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerParity(parityCase{source: newObjectGrantDataSource, resource: newObjectGrantResource, selectors: []string{"database_name", "schema_name", "object_name", "object_type", "arguments", "grantee", "grantee_type"}})

// TestObjectGrantLookup observes one object's explicit group permissions without reconciling extras.
func TestObjectGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newObjectGrantDataSource, map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "table", "object_type": "TABLE", "grantee": "readers", "grantee_type": "GROUP"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})}, &privilegeCatalog{values: map[string]bool{"SELECT": true}, grantee: "readers", kind: "group", scope: "TABLE"})
}
