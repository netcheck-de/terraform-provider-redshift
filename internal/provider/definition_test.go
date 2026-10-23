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

// TestDefinitionFingerprintKeepsQuotedText detects whitespace changes inside literals, quoted identifiers,
// dollar-quoted text, and comments, which Redshift keeps as written, while layout around them stays ignored.
func TestDefinitionFingerprintKeepsQuotedText(t *testing.T) {
	for _, pair := range [][2]string{
		{"SELECT 'A B'", "SELECT 'A  B'"},
		{`SELECT "a b" FROM t`, `SELECT "a  b" FROM t`},
		{"SELECT $$a b$$", "SELECT $$a  b$$"},
		{"SELECT $body$a b$body$", "SELECT $body$a\nb$body$"},
		{"SELECT 1 /* a b */", "SELECT 1 /* a  b */"},
		{"-- c\nSELECT 1", "-- c SELECT 1"},
		{`SELECT 'it''s  a'`, `SELECT 'it''s a'`},
		{`SELECT 'a\'  b'`, `SELECT 'a\' b'`},
		{`SELECT "x""  y"`, `SELECT "x"" y"`},
		{"SELECT 'open  ended", "SELECT 'open ended"},
	} {
		assert.NotEqual(t, definitionFingerprint(pair[0]), definitionFingerprint(pair[1]), "%q vs %q", pair[0], pair[1])
	}
	for _, pair := range [][2]string{
		{"SELECT 'A  B'  FROM t", " SELECT\n'A  B'\tFROM t\n"},
		{"SELECT $1 + 1", "SELECT  $1\n+ 1"},
		{"-- c\nSELECT 1", "-- c\n\n  SELECT   1"},
	} {
		assert.Equal(t, definitionFingerprint(pair[0]), definitionFingerprint(pair[1]), "%q vs %q", pair[0], pair[1])
	}
}

// TestReconcileDefinitionSurfacesQuotedWhitespaceChanges reports an outside change of whitespace inside a literal,
// which Redshift keeps as written, and keeps the surfaced text on the next refresh.
func TestReconcileDefinitionSurfacesQuotedWhitespaceChanges(t *testing.T) {
	configured, stored := recordDefinition(types.StringValue("select 'a b' from t"), "SELECT 'a b'\nFROM t;")
	changedText := "SELECT 'a  b'\nFROM t;"
	definition, fingerprint := reconcileDefinition(configured, stored, changedText)
	assert.Equal(t, types.StringValue(changedText), definition)
	stable, _ := reconcileDefinition(definition, fingerprint, changedText)
	assert.Equal(t, definition, stable)
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
		{"refresh after apply keeps configuration", configured, recorded, catalogText, configured, recorded},
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

// TestRecordDefinition keeps the configured text at apply, where the planned fingerprint is still unknown, and
// stores the fingerprint that the next refresh compares against.
func TestRecordDefinition(t *testing.T) {
	configured := types.StringValue("select a from t")
	catalogText := "SELECT a\nFROM t;"
	definition, stored := recordDefinition(configured, catalogText)
	assert.Equal(t, configured, definition)
	assert.Equal(t, types.StringValue(definitionFingerprint(catalogText)), stored)
	refreshed, storedAgain := reconcileDefinition(definition, stored, "SELECT a FROM t;")
	assert.Equal(t, configured, refreshed, "the first refresh after apply keeps the configured text")
	assert.Equal(t, stored, storedAgain)
	// Passing the unknown plan value to reconcileDefinition instead would replace the configured text.
	surfaced, _ := reconcileDefinition(configured, types.StringUnknown(), catalogText)
	assert.NotEqual(t, configured, surfaced)
}
