package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newViewDataSource, resource: newViewResource, selectors: []string{"database", "schema", "name"}})

// TestViewLookup reads the catalog definition, binding mode, owner, and fingerprint without mutating.
func TestViewLookup(t *testing.T) {
	c, views := viewFake()
	views.get(fakeViewName).lateBinding = true
	definition := views.get(fakeViewName).definition()
	exerciseCatalogLookup(t, newViewDataSource, map[string]string{"database": "admin", "schema": fakeViewSchema, "name": fakeViewName}, map[string]attr.Value{
		"query":                  types.StringValue(definition),
		"late_binding":           types.BoolValue(true),
		"owner":                  types.StringValue(fakeViewOwner),
		"definition_fingerprint": types.StringValue(definitionFingerprint(definition)),
	}, c)
	assert.Empty(t, c.writes)
}

// TestViewLookupRejectsMaterializedView reports a materialized view instead of describing it as a view.
func TestViewLookupRejectsMaterializedView(t *testing.T) {
	c, _ := viewFake()
	source := newViewDataSource()
	_, diagnostics := readSource(t, source, catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": fakeViewSchema, "name": fakeMaterializedViewName}), c)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "materialized view")
}

// TestViewLookupDescriptions keeps the resources' apply, import, and state wording out of both lookups, whose
// outputs only report the catalog.
func TestViewLookupDescriptions(t *testing.T) {
	for _, factory := range []func() datasource.DataSource{newViewDataSource, newMaterializedViewDataSource} {
		var response datasource.SchemaResponse
		factory().Schema(context.Background(), datasource.SchemaRequest{}, &response)
		for name, description := range nestedDescriptions(response.Schema.Attributes) {
			for _, resourceOnly := range []string{"State keeps", "import", "A change runs", "Defaults to", "When set", "Changed in place", "Changing it"} {
				assert.NotContains(t, description, resourceOnly, "%s", name)
			}
		}
	}
}
