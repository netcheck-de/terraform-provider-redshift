package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistry collects per-type fixtures that each type's test file registers from a package-level
// declaration, so adding a type never edits a shared table.
type testRegistry[T any] struct {
	// entries maps a registration key to its fixture.
	entries map[string]T
	// duplicates lists keys registered more than once; the owning coverage test reports them.
	duplicates []string
}

// add records one fixture and returns true for use in `var _ = ...` declarations.
func (r *testRegistry[T]) add(key string, value T) bool {
	if r.entries == nil {
		r.entries = map[string]T{}
	}
	if _, ok := r.entries[key]; ok {
		r.duplicates = append(r.duplicates, key)
	}
	r.entries[key] = value
	return true
}

// keys returns the registered keys in a deterministic order.
func (r *testRegistry[T]) keys() []string {
	return slices.Sorted(maps.Keys(r.entries))
}

// registeredTypeNames returns the full Terraform type names of the provider's resources and data sources.
func registeredTypeNames() (resources, dataSources []string) {
	p := New("test")()
	for _, factory := range p.Resources(context.Background()) {
		resources = append(resources, "redshift_"+registeredResourceName(factory))
	}
	for _, factory := range p.DataSources(context.Background()) {
		dataSources = append(dataSources, "redshift_"+registeredDataSourceName(factory))
	}
	return resources, dataSources
}

// packageTestFunctions lists the Test functions declared in this package, so registrations can name the
// dedicated test that covers what a generic test cannot.
var packageTestFunctions = sync.OnceValues(func() (map[string]bool, error) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	fileSet := token.NewFileSet()
	for _, file := range files {
		parsed, err := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				names[function.Name.Name] = true
			}
		}
	}
	return names, nil
})

// requireTestFunction fails unless this package declares the named Test function.
func requireTestFunction(t *testing.T, name string) {
	t.Helper()
	names, err := packageTestFunctions()
	require.NoError(t, err)
	assert.True(t, names[name], "test function %s does not exist", name)
}

// TestRegistryIsSortedAndUnique checks that the provider lists every type once, in a stable order, and that
// callers cannot alter the registry through the returned slices.
func TestRegistryIsSortedAndUnique(t *testing.T) {
	resources, dataSources := registeredTypeNames()
	for kind, names := range map[string][]string{"resource": resources, "data source": dataSources} {
		assert.True(t, sort.StringsAreSorted(names), "%s types must be sorted: %v", kind, names)
		assert.Len(t, names, len(slices.Compact(slices.Clone(names))), "%s types must be unique: %v", kind, names)
	}
	p := New("test")()
	first := p.Resources(context.Background())
	first[0] = func() resource.Resource { return nil }
	assert.NotNil(t, p.Resources(context.Background())[0]())
	sources := p.DataSources(context.Background())
	sources[0] = func() datasource.DataSource { return nil }
	assert.NotNil(t, p.DataSources(context.Background())[0]())
}

// TestTestRegistryReportsDuplicates checks the shared fixture registry used by the coverage tests.
func TestTestRegistryReportsDuplicates(t *testing.T) {
	var registry testRegistry[int]
	assert.True(t, registry.add("b", 1))
	assert.True(t, registry.add("a", 2))
	assert.Empty(t, registry.duplicates)
	registry.add("b", 3)
	assert.Equal(t, []string{"b"}, registry.duplicates)
	assert.Equal(t, []string{"a", "b"}, registry.keys())
	assert.Equal(t, 3, registry.entries["b"])
	requireTestFunction(t, "TestTestRegistryReportsDuplicates")
}
