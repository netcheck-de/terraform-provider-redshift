package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestDefinitionFingerprint checks that only whitespace layout is ignored.
func TestDefinitionFingerprint(t *testing.T) {
	base := definitionFingerprint("SELECT a, b FROM t WHERE a = 1")
	assert.Len(t, base, 64)
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", definitionFingerprint(" \n\t"), "blank text hashes as empty")
	for _, same := range []string{"SELECT a, b FROM t WHERE a = 1", "  SELECT a,  b\nFROM t\n\tWHERE a = 1\n", "SELECT\ta,\r\nb FROM t WHERE a = 1"} {
		assert.Equal(t, base, definitionFingerprint(same), "%q", same)
	}
	for _, different := range []string{"SELECT a,b FROM t WHERE a = 1", "select a, b from t where a = 1", "SELECT a, b FROM t WHERE a = 2", "SELECT a, b FROM t WHERE a = '1'"} {
		assert.NotEqual(t, base, definitionFingerprint(different), "%q", different)
	}
}

// TestReconcileDefinition checks which text state keeps after apply, refresh, outside change, and import.
func TestReconcileDefinition(t *testing.T) {
	configured := types.StringValue("select a from t")
	catalogText := "SELECT a\nFROM t;"
	recorded := types.StringValue(definitionFingerprint(catalogText))
	changedText := "SELECT a, b\nFROM t;"
	changed := types.StringValue(definitionFingerprint(changedText))
	for _, test := range []struct {
		name                    string
		configured, fingerprint types.String
		catalog                 string
		definition, stored      types.String
	}{
		{"apply records the catalog fingerprint and keeps configuration", configured, recorded, catalogText, configured, recorded},
		{"refresh ignores catalog re-rendering", configured, recorded, "SELECT a FROM t;", configured, recorded},
		{"outside change surfaces the catalog text", configured, recorded, changedText, types.StringValue(changedText), changed},
		{"missing fingerprint surfaces the catalog text", configured, types.StringNull(), catalogText, types.StringValue(catalogText), recorded},
		{"import surfaces the catalog text", types.StringNull(), types.StringNull(), catalogText, types.StringValue(catalogText), recorded},
		{"unknown configuration surfaces the catalog text", types.StringUnknown(), recorded, catalogText, types.StringValue(catalogText), recorded},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition, stored := reconcileDefinition(test.configured, test.fingerprint, test.catalog)
			assert.Equal(t, test.definition, definition)
			assert.Equal(t, test.stored, stored)
		})
	}
	// After surfacing an outside change, the next refresh is stable until the catalog changes again.
	definition, stored := reconcileDefinition(configured, recorded, changedText)
	again, storedAgain := reconcileDefinition(definition, stored, changedText)
	assert.Equal(t, definition, again)
	assert.Equal(t, stored, storedAgain)
	attribute := definitionFingerprintAttribute()
	assert.True(t, attribute.Computed)
	assert.False(t, attribute.Optional)
}
