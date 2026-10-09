package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newUserDataSource, resource: newUserResource, selectors: []string{"name"}})

// TestUserLookup checks non-secret user capability lookup.
func TestUserLookup(t *testing.T) {
	state, diagnostics := readSource(t, newUserDataSource(), userData{Name: types.StringValue("grafana")}, &catalog{user: true, createDB: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data userData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.False(t, data.Superuser.ValueBool())
	assert.True(t, data.CreateDB.ValueBool())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "grafana"})
}
