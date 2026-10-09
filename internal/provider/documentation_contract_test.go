package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocsExistForEveryRegisteredType requires a generated page, a template, and examples for each resource and data source.
func TestDocsExistForEveryRegisteredType(t *testing.T) {
	root := filepath.Join("..", "..")
	p := New("test")()
	var resources, dataSources []string
	for _, factory := range p.Resources(context.Background()) {
		var metadata resource.MetadataResponse
		factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		resources = append(resources, metadata.TypeName)
	}
	for _, factory := range p.DataSources(context.Background()) {
		var metadata datasource.MetadataResponse
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		dataSources = append(dataSources, metadata.TypeName)
	}
	for _, category := range []struct {
		dir      string
		types    []string
		examples []string
	}{
		{"resources", resources, []string{"resource.tf", "import.sh"}},
		{"data-sources", dataSources, []string{"data-source.tf"}},
	} {
		pages, err := filepath.Glob(filepath.Join(root, "docs", category.dir, "*.md"))
		require.NoError(t, err)
		assert.Len(t, pages, len(category.types), "docs/%s must contain exactly one page per registered type", category.dir)
		for _, typeName := range category.types {
			name := strings.TrimPrefix(typeName, "redshift_")
			files := []string{
				filepath.Join("docs", category.dir, name+".md"),
				filepath.Join("templates", category.dir, name+".md.tmpl"),
			}
			for _, example := range category.examples {
				files = append(files, filepath.Join("examples", category.dir, typeName, example))
			}
			for _, file := range files {
				_, err := os.Stat(filepath.Join(root, file))
				assert.NoError(t, err, "%s requires %s", typeName, file)
			}
		}
	}
}
