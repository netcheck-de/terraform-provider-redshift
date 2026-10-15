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

// readCompleteExample concatenates the complete example files matching pattern.
func readCompleteExample(t *testing.T, pattern string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "complete", pattern))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var content []byte
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		content = append(content, data...)
	}
	return string(content)
}

// TestCompleteExampleCoversEverySQLType prevents resource or lookup additions from silently missing the full example.
func TestCompleteExampleCoversEverySQLType(t *testing.T) {
	configuration := readCompleteExample(t, "*.tf")
	// Blocks expose their lookups in their own outputs_<block>.tf, so adding one never edits a shared file.
	outputs := readCompleteExample(t, "outputs*.tf")
	p := New("test")()
	for _, factory := range p.Resources(context.Background()) {
		var metadata resource.MetadataResponse
		factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		assert.Regexp(t, `resource\s+"`+regexp.QuoteMeta(metadata.TypeName)+`"`, configuration, metadata.TypeName)
	}
	for _, factory := range p.DataSources(context.Background()) {
		var metadata datasource.MetadataResponse
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		assert.Regexp(t, `data\s+"`+regexp.QuoteMeta(metadata.TypeName)+`"`, configuration, metadata.TypeName)
		assert.Contains(t, outputs, "data."+metadata.TypeName+".", "lookup results must be exposed: %s", metadata.TypeName)
	}
}
