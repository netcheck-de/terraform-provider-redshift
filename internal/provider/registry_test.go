package provider

import (
	"context"
	"maps"
	"slices"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
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
