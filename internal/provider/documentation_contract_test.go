package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
		heading := sqlSummaryHeadings[category.dir]
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
			template, err := os.ReadFile(filepath.Join(root, "templates", category.dir, name+".md.tmpl"))
			if assert.NoError(t, err) {
				assert.Contains(t, string(template), heading+"\n\n```sql\n", "%s must introduce its SQL summary with %q", typeName, heading)
			}
		}
	}
}

// TestTerraformSnippetsSeparateBlocks requires a blank line between a closing brace and the next block in the
// examples and template snippets, because documentation pages render them as written and adjacent blocks run together.
func TestTerraformSnippetsSeparateBlocks(t *testing.T) {
	root := filepath.Join("..", "..")
	snippets := map[string][]string{}
	examples, err := filepath.Glob(filepath.Join(root, "examples", "*", "*", "*.tf"))
	require.NoError(t, err)
	complete, err := filepath.Glob(filepath.Join(root, "examples", "complete", "*.tf"))
	require.NoError(t, err)
	for _, file := range append(examples, complete...) {
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		snippets[file] = strings.Split(string(content), "\n")
	}
	templates, err := filepath.Glob(filepath.Join(root, "templates", "*", "*.md.tmpl"))
	require.NoError(t, err)
	for _, file := range templates {
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		for i, block := range terraformFence.FindAllStringSubmatch(string(content), -1) {
			snippets[file+"#"+strconv.Itoa(i+1)] = strings.Split(block[1], "\n")
		}
	}
	for name, lines := range snippets {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i-1]) == "}" && blockHeader.MatchString(lines[i]) {
				assert.Fail(t, "blocks need a blank line between them", "%s:%d: %s", name, i+1, strings.TrimSpace(lines[i]))
			}
		}
	}
}

// terraformFence captures the body of fenced Terraform code in a documentation template.
var terraformFence = regexp.MustCompile("(?ms)^```(?:terraform|hcl)\n(.*?)^```")

// blockHeader matches a line that opens a block, such as `column {` or `resource "redshift_table" "events" {`.
var blockHeader = regexp.MustCompile(`^\s*[a-z_]+(\s+"[^"]*")*\s*\{\s*$`)

// sqlSummaryHeadings names the section that holds each page kind's simplified SQL block, so readers know what it shows.
var sqlSummaryHeadings = map[string]string{
	"resources":    "## SQL Statements",
	"data-sources": "## Catalog Query",
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
