package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestAssumeroleGrantLookup reads explicit IAM command grants without enabling access control.
func TestAssumeroleGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newAssumeroleGrantDataSource, map[string]string{"iam_role_arn": "default", "grantee": "readers", "grantee_type": "ROLE"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("COPY")})}, &privilegeCatalog{values: map[string]bool{"COPY": true}})
}
