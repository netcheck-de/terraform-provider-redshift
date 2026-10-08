package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoleReadsCatalogName checks role-name refresh from the SQL catalog.
func TestRoleReadsCatalogName(t *testing.T) {
	c := &catalog{role: true}
	r := &roleResource{testResourceClient(c)}
	data := roleModel{Name: types.StringValue("example:readers")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "example:readers", data.Name.ValueString())
}
