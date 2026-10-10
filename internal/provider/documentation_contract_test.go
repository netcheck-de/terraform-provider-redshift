package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
				assert.FileExists(t, filepath.Join(root, file), "%s requires %s", typeName, file)
			}
		}
	}
}

// TestReconciliationSectionDocumentsEveryInput requires each resource template to explain, before its import
// section, how every configurable argument and block reconciles, so a new argument cannot ship without saying whether
// it updates in place or replaces the object.
func TestReconciliationSectionDocumentsEveryInput(t *testing.T) {
	for _, factory := range New("test")().Resources(context.Background()) {
		instance := factory()
		var metadata resource.MetadataResponse
		instance.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		t.Run(metadata.TypeName, func(t *testing.T) {
			name := strings.TrimPrefix(metadata.TypeName, "redshift_")
			template, err := os.ReadFile(filepath.Join("..", "..", "templates", "resources", name+".md.tmpl"))
			require.NoError(t, err)
			section := reconciliationSection(t, string(template))
			assert.True(t, changeTableHeader.MatchString(section), "the Reconciliation section needs a | Change | Result | table")
			var response resource.SchemaResponse
			instance.Schema(context.Background(), resource.SchemaRequest{}, &response)
			var missing []string
			for _, input := range documentedInputs(response.Schema) {
				if !strings.Contains(section, "`"+input+"`") {
					missing = append(missing, input)
				}
			}
			assert.Empty(t, missing, "the Reconciliation section must name these inputs in backticks")
		})
	}
}

// changeTableHeader matches the header row of the hand-aligned reconciliation table.
var changeTableHeader = regexp.MustCompile(`(?m)^\| Change +\| Result +\|$`)

// reconciliationSection returns the text between the Reconciliation heading and the Import heading that must
// follow it.
func reconciliationSection(t *testing.T, template string) string {
	t.Helper()
	start := strings.Index(template, "\n## Reconciliation\n")
	require.NotEqual(t, -1, start, "template needs a ## Reconciliation section")
	length := strings.Index(template[start:], "\n## Import\n")
	require.NotEqual(t, -1, length, "## Reconciliation must come before ## Import")
	return template[start : start+length]
}

// documentedInputs lists what a reconciliation table must name: every configurable attribute, and every block
// through its configurable fields as block.field at any depth, or by its own name when it has none, since a row about
// `timeouts.create` already names the timeouts block.
func documentedInputs(s schema.Schema) []string {
	var inputs []string
	for name, attribute := range s.Attributes {
		if attribute.IsRequired() || attribute.IsOptional() {
			inputs = append(inputs, name)
		}
	}
	for name, block := range s.Blocks {
		inputs = append(inputs, blockInputs(name, block)...)
	}
	slices.Sort(inputs)
	return inputs
}

// blockInputs lists the configurable fields of a block as dotted paths, or the block itself when it has none.
func blockInputs(path string, block schema.Block) []string {
	var inputs []string
	for name, child := range nestedInputs(block) {
		switch child := child.(type) {
		case schema.Block:
			inputs = append(inputs, blockInputs(path+"."+name, child)...)
		case schema.Attribute:
			if child.IsRequired() || child.IsOptional() {
				inputs = append(inputs, path+"."+name)
			}
		}
	}
	if len(inputs) == 0 {
		return []string{path}
	}
	return inputs
}
