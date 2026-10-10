package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalPartitionDataSource, resource: newExternalPartitionResource, selectors: []string{"database", "schema", "table", "values"}})

// TestExternalPartitionLookup reports the location with the resource identity and fails for a missing partition.
func TestExternalPartitionLookup(t *testing.T) {
	config := externalPartitionTestModel()
	config.Location = types.StringNull()
	state, diagnostics := readSource(t, newExternalPartitionDataSource(), config, fullCatalog())
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data externalPartitionModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, "s3://example-bucket/events/event_date=2024-01-01", data.Location.ValueString())
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events","values":"{\"event_date\":\"2024-01-01\"}"}`, data.ID.ValueString())
	config.Values = externalTableTestMap("event_date", "1999-01-01")
	_, diagnostics = readSource(t, newExternalPartitionDataSource(), config, fullCatalog())
	assert.True(t, diagnostics.HasError())
	_, diagnostics = readSource(t, newExternalPartitionDataSource(), config, &catalog{})
	assert.True(t, diagnostics.HasError(), "a missing table is an error")
}
