package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
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

// TestExternalPartitionLookupReportsMalformedKeys reports undecodable partition key metadata under the lookup's summary.
func TestExternalPartitionLookupReportsMalformedKeys(t *testing.T) {
	_, diagnostics := readSource(t, newExternalPartitionDataSource(), externalPartitionTestModel(), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"columnname": "day", "external_type": "date", "columnnum": "invalid", "part_key": "1"}}, nil
	}))
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Read external partition", diagnostics.Errors()[0].Summary())
}
