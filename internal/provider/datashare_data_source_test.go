package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatashareLookup verifies outbound share lookup attributes.
func TestDatashareLookup(t *testing.T) {
	state, diagnostics := readSource(t, newDatashareDataSource(), datashareData{Name: types.StringValue("producer"), Database: types.StringValue("admin")}, &catalog{share: true, public: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data datashareData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.True(t, data.PublicAccessible.ValueBool())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "producer"})
}
