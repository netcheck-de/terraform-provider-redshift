package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExternalSchemaLookup checks Glue database and IAM role lookup attributes.
func TestExternalSchemaLookup(t *testing.T) {
	state, diagnostics := readSource(t, newExternalSchemaDataSource(), externalSchemaData{Database: types.StringValue("admin"), Name: types.StringValue("example_external")}, &catalog{external: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data externalSchemaData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "example_glue", data.GlueDatabase.ValueString())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "example_external"})
}
