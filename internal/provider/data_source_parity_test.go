package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parityCase pairs a data source with the resource whose readable attributes it mirrors.
type parityCase struct {
	// source constructs the data source under test.
	source func() datasource.DataSource
	// resource constructs the paired resource; a collection without a managing resource leaves it nil.
	resource func() resource.Resource
	// selectors are the lookup inputs, which keep the resource's required/optional flags.
	selectors []string
	// collection marks a listing whose elements, not its top level, mirror the resource.
	collection bool
	// filters are a collection's optional inputs.
	filters []string
}

// parityCases lists every registered data source's parity contract.
var parityCases []parityCase

// registerParity declares a data source's parity contract from its own test file.
func registerParity(test parityCase) bool {
	parityCases = append(parityCases, test)
	return true
}

// resourcesWithoutLookup maps resource types that deliberately have no single-object lookup to the reason.
var resourcesWithoutLookup testRegistry[string]

// registerLookupExemption records why a resource type has no single-object lookup.
func registerLookupExemption(typeName, reason string) bool {
	return resourcesWithoutLookup.add(typeName, reason)
}

// TestLookupExemptionRegistry checks that exemptions keep their reason and report duplicates.
func TestLookupExemptionRegistry(t *testing.T) {
	saved := resourcesWithoutLookup
	t.Cleanup(func() { resourcesWithoutLookup = saved })
	resourcesWithoutLookup = testRegistry[string]{}
	assert.True(t, registerLookupExemption("redshift_example", "listed by redshift_examples only"))
	assert.Equal(t, "listed by redshift_examples only", resourcesWithoutLookup.entries["redshift_example"])
	registerLookupExemption("redshift_example", "again")
	assert.Equal(t, []string{"redshift_example"}, resourcesWithoutLookup.duplicates)
}

// readableAttributes returns the resource attributes a lookup can observe.
func readableAttributes(factory func() resource.Resource) map[string]schema.Attribute {
	var response resource.SchemaResponse
	factory().Schema(context.Background(), resource.SchemaRequest{}, &response)
	attributes := map[string]schema.Attribute{}
	for name, attribute := range response.Schema.Attributes {
		if !lookupExcluded(name, attribute) {
			attributes[name] = attribute
		}
	}
	return attributes
}

// dataSourceSchema returns a data source's schema.
func dataSourceSchema(factory func() datasource.DataSource) datasourceschema.Schema {
	var response datasource.SchemaResponse
	factory().Schema(context.Background(), datasource.SchemaRequest{}, &response)
	return response.Schema
}

// assertLookupParity checks a single-object lookup against its paired resource.
func assertLookupParity(t *testing.T, test parityCase) {
	t.Helper()
	source := dataSourceSchema(test.source)
	var paired resource.SchemaResponse
	test.resource().Schema(context.Background(), resource.SchemaRequest{}, &paired)
	for name, attribute := range paired.Schema.Attributes {
		if lookupExcluded(name, attribute) {
			assert.NotContains(t, source.Attributes, name)
		}
	}
	for name, expected := range readableAttributes(test.resource) {
		actual, ok := source.Attributes[name]
		require.True(t, ok, "missing readable attribute %s", name)
		assert.Equal(t, expected.GetType(), actual.GetType(), name)
		// Observed values are computed even when their resource counterpart is configurable.
		if slices.Contains(test.selectors, name) {
			assert.Equal(t, expected.IsRequired(), actual.IsRequired(), name)
			assert.Equal(t, expected.IsOptional(), actual.IsOptional(), name)
			assert.False(t, actual.IsComputed(), name)
		} else {
			assert.True(t, actual.IsComputed(), name)
			assert.False(t, actual.IsRequired(), name)
			assert.False(t, actual.IsOptional(), name)
		}
	}
	for name := range source.Attributes {
		if name != "exists" {
			assert.Contains(t, paired.Schema.Attributes, name, "unpaired data-source attribute")
		}
	}
}

// assertCollectionParity checks a listing's filters and that its element type is a computed subset of the
// paired resource's readable attributes.
func assertCollectionParity(t *testing.T, test parityCase) {
	t.Helper()
	source := dataSourceSchema(test.source)
	var readable map[string]schema.Attribute
	if test.resource != nil {
		readable = readableAttributes(test.resource)
	}
	expected := append([]string{"id", collectionItems}, test.filters...)
	for name, attribute := range source.Attributes {
		require.Contains(t, expected, name, "collection attributes are id, %s, and the declared filters", collectionItems)
		if !slices.Contains(test.filters, name) {
			continue
		}
		assert.True(t, attribute.IsOptional(), "filter %s must be optional", name)
		assert.False(t, attribute.IsComputed(), "filter %s must not be computed", name)
		if paired, ok := readable[name]; ok {
			assert.Equal(t, paired.GetType(), attribute.GetType(), "filter %s", name)
		}
	}
	require.Contains(t, source.Attributes, "id")
	assert.True(t, source.Attributes["id"].IsComputed())
	items, ok := source.Attributes[collectionItems].(datasourceschema.ListNestedAttribute)
	require.True(t, ok, "%s must be a list of nested objects", collectionItems)
	assert.True(t, items.IsComputed())
	require.NotEmpty(t, items.NestedObject.Attributes)
	for name, element := range items.NestedObject.Attributes {
		assert.True(t, element.IsComputed(), "element attribute %s must be computed", name)
		if test.resource == nil {
			continue
		}
		paired, ok := readable[name]
		if assert.True(t, ok, "element attribute %s is not a readable resource attribute", name) {
			assert.Equal(t, paired.GetType(), element.GetType(), name)
		}
	}
}

// TestDataSourceReadableAttributeParity checks all lookup schemas against their paired resources.
func TestDataSourceReadableAttributeParity(t *testing.T) {
	require.Empty(t, resourcesWithoutLookup.duplicates, "duplicate lookup exemptions")
	resources, dataSources := registeredTypeNames()
	covered, lookups := map[string]bool{}, map[string]bool{}
	for _, test := range parityCases {
		name := "redshift_" + registeredDataSourceName(test.source)
		require.False(t, covered[name], "duplicate parity case for %s", name)
		covered[name] = true
		if !test.collection && test.resource != nil {
			lookups["redshift_"+registeredResourceName(test.resource)] = true
		}
		t.Run(name, func(t *testing.T) {
			if test.collection {
				assertCollectionParity(t, test)
				return
			}
			require.NotNil(t, test.resource, "a single-object lookup needs a paired resource")
			require.Empty(t, test.filters, "filters apply only to collections")
			assertLookupParity(t, test)
		})
	}
	for _, name := range dataSources {
		assert.True(t, covered[name], "data source %s needs a parity case", name)
	}
	assert.Len(t, covered, len(dataSources), "parity cases must name registered data sources")
	for _, name := range resources {
		_, exempt := resourcesWithoutLookup.entries[name]
		assert.NotEqual(t, lookups[name], exempt, "resource %s needs exactly one of a lookup or a lookup exemption", name)
	}
	for _, name := range resourcesWithoutLookup.keys() {
		assert.Contains(t, resources, name, "lookup exemption for unregistered resource")
	}
}
