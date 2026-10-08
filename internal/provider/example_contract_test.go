package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompleteExampleCoversEverySQLType prevents resource or lookup additions from silently missing the full example.
func TestCompleteExampleCoversEverySQLType(t *testing.T) {
	configuration, err := os.ReadFile(filepath.Join("..", "..", "examples", "complete", "main.tf"))
	require.NoError(t, err)
	outputs, err := os.ReadFile(filepath.Join("..", "..", "examples", "complete", "outputs.tf"))
	require.NoError(t, err)
	p := New("test")()
	for _, factory := range p.Resources(context.Background()) {
		var metadata resource.MetadataResponse
		factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		assert.Regexp(t, `resource\s+"`+regexp.QuoteMeta(metadata.TypeName)+`"`, string(configuration), metadata.TypeName)
	}
	for _, factory := range p.DataSources(context.Background()) {
		var metadata datasource.MetadataResponse
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		assert.Regexp(t, `data\s+"`+regexp.QuoteMeta(metadata.TypeName)+`"`, string(configuration), metadata.TypeName)
		assert.Contains(t, string(outputs), "data."+metadata.TypeName+".", "lookup results must be exposed: %s", metadata.TypeName)
	}
}
