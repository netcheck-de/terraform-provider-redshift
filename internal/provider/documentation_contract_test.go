package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// documentationReferenceFields collects explicitly documented fields while ignoring fenced examples and other sections.
func documentationReferenceFields(document string) map[string]map[string]bool {
	fields := map[string]map[string]bool{
		"Argument Reference":  {},
		"Attribute Reference": {},
	}
	var section, fence string
	for _, line := range strings.Split(document, "\n") {
		line = strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fence = line[:3]
			continue
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			continue
		}
		if fields[section] == nil {
			continue
		}
		cell := strings.TrimSpace(strings.TrimPrefix(line, "|"))
		if !strings.HasPrefix(cell, "`") {
			continue
		}
		name, description, ok := strings.Cut(strings.TrimPrefix(cell, "`"), "`")
		if ok && (strings.HasPrefix(line, "|") || strings.HasPrefix(strings.TrimSpace(description), "(")) {
			fields[section][name] = true
		}
	}
	return fields
}

// undocumentedFields reports named and embedded struct/interface members lacking documentation.
func undocumentedFields(typeName string, fields *ast.FieldList) []string {
	var missing []string
	for _, field := range fields.List {
		if field.Doc != nil || field.Comment != nil {
			continue
		}
		if len(field.Names) == 0 {
			missing = append(missing, typeName+".<embedded>")
		}
		for _, name := range field.Names {
			missing = append(missing, typeName+"."+name.Name)
		}
	}
	return missing
}

// TestDocumentationReferencesDoNotRepeatArguments enforces disjoint input and computed-output references on every page.
func TestDocumentationReferencesDoNotRepeatArguments(t *testing.T) {
	p := New("test")()
	for _, category := range []struct {
		name  string
		count int
	}{
		{"resources", len(p.Resources(context.Background()))},
		{"data-sources", len(p.DataSources(context.Background()))},
	} {
		pages, err := filepath.Glob(filepath.Join("..", "..", "docs", category.name, "*.md"))
		require.NoError(t, err)
		require.Len(t, pages, category.count)
		for _, page := range pages {
			t.Run(category.name+"/"+filepath.Base(page), func(t *testing.T) {
				contents, err := os.ReadFile(page)
				require.NoError(t, err)
				fields := documentationReferenceFields(string(contents))
				require.NotEmpty(t, fields["Argument Reference"], "inputs must be documented")
				require.NotEmpty(t, fields["Attribute Reference"], "computed outputs must be documented")
				for argument := range fields["Argument Reference"] {
					assert.NotContains(t, fields["Attribute Reference"], argument, "document configurable inputs only in Argument Reference")
				}
			})
		}
	}
}

// TestDocumentationReferenceFieldsIgnoreExamples verifies the contract identifies field definitions rather than cross-references.
func TestDocumentationReferenceFieldsIgnoreExamples(t *testing.T) {
	contents := strings.Join([]string{
		"## Argument Reference", "| `name` | String, required | Name. |",
		"| `enabled` | Boolean, optional/computed | Flag. |",
		"```markdown", "## Attribute Reference", "| `name` | String | Example. |", "```",
		"## Attribute Reference", "| `id` | String | Identity of `name`. |",
		"`region` (String, computed) is the observed region.",
		"`name`, `enabled`, and other inputs appear inside identity descriptions.",
		"## Import", "| `name` | String | Import example. |",
		"~~~markdown", "## Argument Reference", "| `id` | String | Example. |", "~~~",
	}, "\n")
	assert.Equal(t, map[string]map[string]bool{
		"Argument Reference":  {"name": true, "enabled": true},
		"Attribute Reference": {"id": true, "region": true},
	}, documentationReferenceFields(contents))
	fields := documentationReferenceFields(contents + "\n## Attribute Reference\n| `enabled` | Boolean | Repeated input. |")
	assert.True(t, fields["Argument Reference"]["enabled"])
	assert.True(t, fields["Attribute Reference"]["enabled"], "the contract must detect duplicated optional/computed inputs")
}

// TestGoDeclarationsHaveDocumentation covers functions, named types, fields, and package settings, including tests.
func TestGoDeclarationsHaveDocumentation(t *testing.T) {
	root := filepath.Join("..", "..")
	var missing []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Doc == nil {
					missing = append(missing, path+": "+declaration.Name.Name)
				}
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					switch specification := specification.(type) {
					case *ast.TypeSpec:
						label := path + ": " + specification.Name.Name
						if declaration.Doc == nil && specification.Doc == nil {
							missing = append(missing, label)
						}
						switch definition := specification.Type.(type) {
						case *ast.StructType:
							missing = append(missing, undocumentedFields(label, definition.Fields)...)
						case *ast.InterfaceType:
							missing = append(missing, undocumentedFields(label, definition.Methods)...)
						}
					case *ast.ValueSpec:
						if declaration.Doc == nil && specification.Doc == nil {
							for _, name := range specification.Names {
								if name.Name != "_" {
									missing = append(missing, path+": "+name.Name)
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, missing, "all declarations and named type members need documentation")
}
