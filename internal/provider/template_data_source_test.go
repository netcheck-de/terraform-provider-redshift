package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newTemplateDataSource, resource: newTemplateResource, selectors: []string{"database", "schema", "name"}})

var _ = registerParity(parityCase{source: newTemplatesDataSource, resource: newTemplateResource, collection: true, filters: []string{"database", "schema"}})

// TestTemplateLookup observes the catalog format, parameters, and owner without owning the template.
func TestTemplateLookup(t *testing.T) {
	exerciseCatalogLookup(t, newTemplateDataSource, map[string]string{"database": "admin", "schema": templateFakeSchema, "name": "csv_load"}, map[string]attr.Value{
		"owner":           types.StringValue("admin"),
		"format":          types.StringValue("CSV"),
		"format_argument": types.StringNull(),
		"parameters":      templateLifecycleModel.Parameters,
	}, fullCatalog())
}

// TestTemplatesListing lists one schema or every local schema, sorted by name, and reports catalog errors.
func TestTemplatesListing(t *testing.T) {
	for _, filters := range []map[string]string{{}, {"schema": templateFakeSchema}, {"database": "admin", "schema": templateFakeSchema}} {
		source := newTemplatesDataSource()
		state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), fullCatalog())
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var data types.Object
		require.False(t, state.Get(context.Background(), &data).HasError())
		items := collectionResult(source, data).Elements()
		require.Len(t, items, 2)
		names := []string{objectString(items[0].(types.Object), "name"), objectString(items[1].(types.Object), "name")}
		assert.Equal(t, []string{"csv_load", templateFakeDecoy}, names)
		assert.Equal(t, templateFakeSchema, objectString(items[0].(types.Object), "schema"))
		assert.Equal(t, "admin", objectString(items[0].(types.Object), "owner"))
		assert.Equal(t, "admin", objectString(items[0].(types.Object), "database"))
	}
	source := newTemplatesDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError())
}

func TestTemplatesListingPreservesQuotedWhitespace(t *testing.T) {
	source := newTemplatesDataSource()
	client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"schema_name": " leading schema ", "template_name": " leading template ", "template_owner": " leading owner "}}, nil
	})
	state, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())
	items := collectionResult(source, data).Elements()
	require.Len(t, items, 1)
	item := items[0].(types.Object)
	assert.Equal(t, " leading schema ", objectString(item, "schema"))
	assert.Equal(t, " leading template ", objectString(item, "name"))
	assert.Equal(t, " leading owner ", objectString(item, "owner"))
}
