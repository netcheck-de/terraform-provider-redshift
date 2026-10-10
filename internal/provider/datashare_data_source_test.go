package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareDataSource, resource: newDatashareResource, selectors: []string{"database", "name"}})

// TestDatashareLookup verifies outbound share lookup attributes.
func TestDatashareLookup(t *testing.T) {
	state, diagnostics := readSource(t, newDatashareDataSource(), datashareData{Name: types.StringValue("producer"), Database: types.StringValue("admin")}, &catalog{share: true, public: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data datashareData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.True(t, data.PublicAccessible.ValueBool())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "producer"})
}

// TestDatashareLookupObservesCatalogMetadata exposes the same owner and producer attributes as the resource.
func TestDatashareLookupObservesCatalogMetadata(t *testing.T) {
	state, diagnostics := readSource(t, newDatashareDataSource(), datashareData{Name: types.StringValue("producer"), Database: types.StringValue("admin")}, fullCatalog())
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data datashareData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, types.StringValue("admin"), data.Owner)
	assert.Equal(t, types.Int64Value(100), data.ShareID)
	assert.Equal(t, types.StringValue("123456789012"), data.ProducerAccount)
	assert.Equal(t, types.StringValue("11111111-2222-3333-4444-555555555555"), data.ProducerNamespace)
	assert.Equal(t, types.StringValue("2026-01-02 03:04:05"), data.CreatedAt)
}
