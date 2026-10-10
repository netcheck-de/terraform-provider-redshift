package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newAssumeroleGrantDataSource, resource: newAssumeroleGrantResource, selectors: []string{"iam_role_arn", "grantee", "grantee_type"}})

// TestAssumeroleGrantLookup reads explicit IAM command grants without enabling access control.
func TestAssumeroleGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newAssumeroleGrantDataSource, map[string]string{"iam_role_arn": "DEFAULT", "grantee": "readers", "grantee_type": "ROLE"}, map[string]attr.Value{"privileges": types.SetValueMust(types.StringType, []attr.Value{types.StringValue("COPY")})}, &privilegeCatalog{values: map[string]bool{"COPY": true}})
}

// TestAssumeroleGrantLookupCanonicalIdentity accepts the DEFAULT keyword in any case, keeps it as configured, and
// records the canonical spelling in the identity, as Create does; an ARN is recorded byte-exact.
func TestAssumeroleGrantLookupCanonicalIdentity(t *testing.T) {
	for configured, canonical := range map[string]string{"default": "DEFAULT", "All": "ALL", "arn:aws:iam::123456789012:role/Loader": "arn:aws:iam::123456789012:role/Loader"} {
		fields := map[string]string{"iam_role_arn": configured, "grantee": "readers", "grantee_type": "ROLE"}
		source := newAssumeroleGrantDataSource()
		state, diagnostics := readSource(t, source, catalogLookupObject(t, source, fields), &privilegeCatalog{values: map[string]bool{"COPY": true}})
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var observed types.Object
		require.False(t, state.Get(context.Background(), &observed).HasError())
		assert.Equal(t, types.StringValue(configured), observed.Attributes()["iam_role_arn"])
		fields["iam_role_arn"] = canonical
		assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", fields)
	}
}
