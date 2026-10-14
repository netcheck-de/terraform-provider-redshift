package sqlclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokens splits server-printed SQL into words, decoded literals and identifiers, and punctuation.
func TestTokens(t *testing.T) {
	tokens, err := Tokens(`CREATE OR REPLACE TEMPLATE dev."Odd""Schema".t$1 FOR COPY AS FORMAT AS JSON 'auto' NULL '' DELIMITER 'it''s \\ \' \t' /* note */ MAXERROR 100; -- done`)
	require.NoError(t, err)
	assert.Equal(t, []Token{
		{TokenWord, "CREATE"}, {TokenWord, "OR"}, {TokenWord, "REPLACE"}, {TokenWord, "TEMPLATE"},
		{TokenWord, "dev"}, {TokenSymbol, "."}, {TokenIdentifier, `Odd"Schema`}, {TokenSymbol, "."}, {TokenWord, "t$1"},
		{TokenWord, "FOR"}, {TokenWord, "COPY"}, {TokenWord, "AS"}, {TokenWord, "FORMAT"}, {TokenWord, "AS"}, {TokenWord, "JSON"},
		{TokenString, "auto"}, {TokenWord, "NULL"}, {TokenString, ""}, {TokenWord, "DELIMITER"}, {TokenString, `it's \ ' \t`},
		{TokenWord, "MAXERROR"}, {TokenWord, "100"}, {TokenSymbol, ";"},
	}, tokens)
	// Literal and Tokens round-trip any value.
	for _, value := range []string{"", "plain", `it's`, `C:\path\`, `\t`, "multi\nline"} {
		tokens, err := Tokens(Literal(value))
		require.NoError(t, err)
		assert.Equal(t, []Token{{TokenString, value}}, tokens, value)
	}
	for _, text := range []string{"'open", `"open`, "/* open", "$$body", "x = :name"} {
		_, err := Tokens(text)
		require.Error(t, err, text)
	}
}
