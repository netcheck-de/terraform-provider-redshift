package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDebugGrantDataSource, resource: newDebugGrantResource, selectors: []string{"database", "grantee", "grantee_type"}})

// TestDebugGrantLookup checks an existing grant, its identity, absence, and read-only execution.
func TestDebugGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDebugGrantDataSource,
		map[string]string{"database": "warehouse", "grantee": "IAM:sales_auditor", "grantee_type": "USER"},
		map[string]attr.Value{"exists": types.BoolValue(true)}, debugGrantCatalog())
}

// TestDebugGrantLookupOtherGrantee reports another grantee of the database as absent rather than as an error.
func TestDebugGrantLookupOtherGrantee(t *testing.T) {
	source := newDebugGrantDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": debugGrantTestDatabase, "grantee": "myidc:audit_team", "grantee_type": "ROLE"})
	state, diagnostics := readSource(t, source, data, debugGrantCatalog())
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, types.BoolValue(false), observed.Attributes()["exists"])
	assert.Equal(t, types.StringNull(), observed.Attributes()["id"])
}
