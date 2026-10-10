package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newSchemaDataSource, resource: newSchemaResource, selectors: []string{"database", "name"}})

// TestSchemaLookup checks local schema lookup and owner refresh.
func TestSchemaLookup(t *testing.T) {
	state, diagnostics := readSource(t, newSchemaDataSource(), schemaData{Database: types.StringValue("admin"), Name: types.StringValue("serving")}, &catalog{schema: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data schemaData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "admin", data.Owner.ValueString())
	assert.Equal(t, int64(-1), data.Quota.ValueInt64(), "a schema without a quota reports UNLIMITED")
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "serving"})
}
