package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newMaterializedViewDataSource, resource: newMaterializedViewResource, selectors: []string{"database", "schema", "name"}})

// TestMaterializedViewLookup reads the definition, refresh setting, and owner, and leaves the storage options
// that no documented catalog view reports null.
func TestMaterializedViewLookup(t *testing.T) {
	c, views := viewFake()
	definition := views.get(fakeMaterializedViewName).definition()
	exerciseCatalogLookup(t, newMaterializedViewDataSource, map[string]string{"database": "admin", "schema": fakeViewSchema, "name": fakeMaterializedViewName}, map[string]attr.Value{
		"query":                  types.StringValue(definition),
		"auto_refresh":           types.BoolValue(true),
		"owner":                  types.StringValue(fakeViewOwner),
		"definition_fingerprint": types.StringValue(definitionFingerprint(definition)),
		"backup":                 types.BoolNull(),
		"distribution":           types.ObjectNull(materializedViewDistributionTypes),
		"sort_key":               types.ObjectNull(materializedViewSortKeyTypes),
	}, c)
	assert.Empty(t, c.writes)
}

// TestMaterializedViewLookupHiddenRefreshSetting reports a refresh setting SVV_MV_INFO hides as null rather
// than failing the lookup.
func TestMaterializedViewLookupHiddenRefreshSetting(t *testing.T) {
	c, views := viewFake()
	definition := views.get(fakeMaterializedViewName).definition()
	exerciseCatalogLookup(t, newMaterializedViewDataSource, map[string]string{"database": "admin", "schema": fakeViewSchema, "name": fakeMaterializedViewName}, map[string]attr.Value{
		"query":                  types.StringValue(definition),
		"auto_refresh":           types.BoolNull(),
		"owner":                  types.StringValue(fakeViewOwner),
		"definition_fingerprint": types.StringValue(definitionFingerprint(definition)),
		"backup":                 types.BoolNull(),
		"distribution":           types.ObjectNull(materializedViewDistributionTypes),
		"sort_key":               types.ObjectNull(materializedViewSortKeyTypes),
	}, materializedViewHiddenRefresh(c))
}

// TestMaterializedViewLookupRejectsOrdinaryView reports an ordinary view instead of describing it as materialized.
func TestMaterializedViewLookupRejectsOrdinaryView(t *testing.T) {
	c, _ := viewFake()
	source := newMaterializedViewDataSource()
	_, diagnostics := readSource(t, source, catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": fakeViewSchema, "name": fakeViewName}), c)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "redshift_view")
}
