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
	// lookupSelectors are inputs only the lookup has (catalogSpec.selectors), such as argument types selecting an
	// overload whose arguments the resource configures as blocks.
	lookupSelectors []string
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

// resourceBlocks returns a resource's blocks, which a lookup always observes.
func resourceBlocks(factory func() resource.Resource) map[string]schema.Block {
	var response resource.SchemaResponse
	factory().Schema(context.Background(), resource.SchemaRequest{}, &response)
	return response.Schema.Blocks
}

// dataSourceSchema returns a data source's schema.
func dataSourceSchema(factory func() datasource.DataSource) datasourceschema.Schema {
	var response datasource.SchemaResponse
	factory().Schema(context.Background(), datasource.SchemaRequest{}, &response)
	return response.Schema
}

// assertBlockParity checks that a lookup observes a resource block as a computed nested attribute of the same kind,
// recursively, so data.x.column[0].name addresses what resource.x.column[0].name does. Types are compared per child
// because the observed object omits attributes the lookup cannot read, such as write-only secrets.
func assertBlockParity(t *testing.T, path string, block schema.Block, observed datasourceschema.Attribute) {
	t.Helper()
	assert.True(t, observed.IsComputed(), "%s must be computed", path)
	assert.False(t, observed.IsRequired() || observed.IsOptional(), "%s must not be configurable", path)
	var attributes map[string]schema.Attribute
	var blocks map[string]schema.Block
	var nested map[string]datasourceschema.Attribute
	switch block := block.(type) {
	case schema.ListNestedBlock:
		list, ok := observed.(datasourceschema.ListNestedAttribute)
		require.True(t, ok, "%s: a list block is observed as a list of nested objects", path)
		attributes, blocks, nested = block.NestedObject.Attributes, block.NestedObject.Blocks, list.NestedObject.Attributes
	case schema.SetNestedBlock:
		set, ok := observed.(datasourceschema.SetNestedAttribute)
		require.True(t, ok, "%s: a set block is observed as a set of nested objects", path)
		attributes, blocks, nested = block.NestedObject.Attributes, block.NestedObject.Blocks, set.NestedObject.Attributes
	case schema.SingleNestedBlock:
		single, ok := observed.(datasourceschema.SingleNestedAttribute)
		require.True(t, ok, "%s: a single block is observed as a single nested object", path)
		attributes, blocks, nested = block.Attributes, block.Blocks, single.Attributes
	default:
		t.Fatalf("%s: add parity coverage for block type %T", path, block)
	}
	for name, child := range blocks {
		actual, ok := nested[name]
		if assert.True(t, ok, "missing readable block %s.%s", path, name) {
			assertBlockParity(t, path+"."+name, child, actual)
		}
	}
	for name, attribute := range attributes {
		actual, ok := nested[name]
		if lookupExcluded(name, attribute) {
			assert.False(t, ok, "%s.%s cannot be observed", path, name)
			continue
		}
		if assert.True(t, ok, "missing readable attribute %s.%s", path, name) {
			assert.Equal(t, attribute.GetType(), actual.GetType(), "%s.%s", path, name)
			assert.True(t, actual.IsComputed(), "%s.%s must be computed", path, name)
		}
	}
	for name := range nested {
		_, attribute := attributes[name]
		_, child := blocks[name]
		assert.True(t, attribute || child, "unpaired nested attribute %s.%s", path, name)
	}
}

// assertLookupParity checks a single-object lookup against its paired resource.
func assertLookupParity(t *testing.T, test parityCase) {
	t.Helper()
	source := dataSourceSchema(test.source)
	assert.Empty(t, source.Blocks, "data sources never declare blocks")
	var paired resource.SchemaResponse
	test.resource().Schema(context.Background(), resource.SchemaRequest{}, &paired)
	for name, block := range paired.Schema.Blocks {
		assert.NotContains(t, test.selectors, name, "block %s is always observed; select with a lookup selector", name)
		if actual, ok := source.Attributes[name]; assert.True(t, ok, "missing readable block %s", name) {
			assertBlockParity(t, name, block, actual)
		}
	}
	for _, name := range test.lookupSelectors {
		assert.NotContains(t, paired.Schema.Attributes, name, "lookup selector %s shadows a resource attribute", name)
		assert.NotContains(t, paired.Schema.Blocks, name, "lookup selector %s shadows a resource block", name)
		if actual, ok := source.Attributes[name]; assert.True(t, ok, "missing lookup selector %s", name) {
			assert.True(t, actual.IsRequired() || actual.IsOptional(), "lookup selector %s must be an input", name)
			assert.False(t, actual.IsComputed(), "lookup selector %s must not be computed", name)
		}
	}
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
		_, attribute := paired.Schema.Attributes[name]
		_, block := paired.Schema.Blocks[name]
		assert.True(t, attribute || block || name == "exists" || slices.Contains(test.lookupSelectors, name), "unpaired data-source attribute %s", name)
	}
}

// assertCollectionParity checks a listing's filters, that its result attribute is named after the data source, and
// that its element type is a computed subset of the paired resource's readable attributes and blocks.
func assertCollectionParity(t *testing.T, test parityCase) {
	t.Helper()
	source := dataSourceSchema(test.source)
	assert.Empty(t, source.Blocks, "data sources never declare blocks")
	var readable map[string]schema.Attribute
	var blocks map[string]schema.Block
	if test.resource != nil {
		readable, blocks = readableAttributes(test.resource), resourceBlocks(test.resource)
	}
	result := registeredDataSourceName(test.source)
	expected := append([]string{"id", result}, test.filters...)
	for name, attribute := range source.Attributes {
		require.Contains(t, expected, name, "collection attributes are id, %s, and the declared filters", result)
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
	items, ok := source.Attributes[result].(datasourceschema.ListNestedAttribute)
	require.True(t, ok, "%s must be a list of nested objects", result)
	assert.True(t, items.IsComputed())
	require.NotEmpty(t, items.NestedObject.Attributes)
	for name, element := range items.NestedObject.Attributes {
		assert.True(t, element.IsComputed(), "element attribute %s must be computed", name)
		if test.resource == nil {
			continue
		}
		if block, ok := blocks[name]; ok {
			assertBlockParity(t, name, block, element)
			continue
		}
		paired, ok := readable[name]
		if assert.True(t, ok, "element attribute %s is not a readable resource attribute", name) {
			assert.Equal(t, paired.GetType(), element.GetType(), name)
		}
	}
}

// TestBlockLookupParity proves the block and lookup-only selector checks on the block test resource.
func TestBlockLookupParity(t *testing.T) {
	assertLookupParity(t, parityCase{source: blockTestLookup, resource: newBlockTestResource, selectors: []string{"database", "name"}, lookupSelectors: []string{"column_types"}})
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
				require.Empty(t, test.lookupSelectors, "lookup selectors apply only to single-object lookups")
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
