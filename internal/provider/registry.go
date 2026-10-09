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
	factories := slices.Clone(resourceFactories)
	slices.SortStableFunc(factories, func(a, b func() resource.Resource) int {
		return strings.Compare(registeredResourceName(a), registeredResourceName(b))
	})
	return factories
}

// registeredDataSources returns a fresh slice sorted by type name; see registeredResources.
func registeredDataSources() []func() datasource.DataSource {
	factories := slices.Clone(dataSourceFactories)
	slices.SortStableFunc(factories, func(a, b func() datasource.DataSource) int {
		return strings.Compare(registeredDataSourceName(a), registeredDataSourceName(b))
	})
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
