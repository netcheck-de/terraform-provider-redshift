package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// resourceFactories and dataSourceFactories are filled during package initialization by each type's own
// file, so adding a type never edits a shared list.
var (
	resourceFactories   []func() resource.Resource
	dataSourceFactories []func() datasource.DataSource
)

// registerResource adds a resource factory to the provider; it returns true so type files can call it from a
// package-level variable declaration.
func registerResource(factory func() resource.Resource) bool {
	resourceFactories = append(resourceFactories, factory)
	return true
}

// registerDataSource adds a data-source factory to the provider; see registerResource.
func registerDataSource(factory func() datasource.DataSource) bool {
	dataSourceFactories = append(dataSourceFactories, factory)
	return true
}

// registeredResources returns a fresh slice sorted by type name, because package initialization order across
// files is an implementation detail and callers may modify the result.
func registeredResources() []func() resource.Resource {
	return sortedFactories(resourceFactories, registeredResourceName)
}

// registeredDataSources returns a fresh slice sorted by type name; see registeredResources.
func registeredDataSources() []func() datasource.DataSource {
	return sortedFactories(dataSourceFactories, registeredDataSourceName)
}

// sortedFactories orders factories by type name. Factories can build entire schemas, so discover names once instead of
// inside every sort comparison.
func sortedFactories[T any](registered []T, name func(T) string) []T {
	type namedFactory struct {
		factory T
		name    string
	}
	entries := make([]namedFactory, len(registered))
	for i, factory := range registered {
		entries[i] = namedFactory{factory: factory, name: name(factory)}
	}
	slices.SortStableFunc(entries, func(a, b namedFactory) int {
		return strings.Compare(a.name, b.name)
	})
	factories := slices.Clone(registered)
	for i, entry := range entries {
		factories[i] = entry.factory
	}
	return factories
}

// registeredResourceName returns a resource's type name without the provider prefix.
func registeredResourceName(factory func() resource.Resource) string {
	var metadata resource.MetadataResponse
	factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	return strings.TrimPrefix(metadata.TypeName, "redshift_")
}

// registeredDataSourceName returns a data source's type name without the provider prefix.
func registeredDataSourceName(factory func() datasource.DataSource) string {
	var metadata datasource.MetadataResponse
	factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	return strings.TrimPrefix(metadata.TypeName, "redshift_")
}
