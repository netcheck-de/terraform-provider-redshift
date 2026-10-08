package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestSystemGrantLookup observes explicit SQL capabilities without changing role privileges.
func TestSystemGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newSystemGrantDataSource, map[string]string{"role": "readers"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("CREATE USER")})}, &privilegeCatalog{values: map[string]bool{"CREATE USER": true}})
}
