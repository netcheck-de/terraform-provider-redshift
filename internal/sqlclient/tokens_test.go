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
		{Kind: TokenWord, Text: "CREATE"}, {Kind: TokenWord, Text: "OR"}, {Kind: TokenWord, Text: "REPLACE"}, {Kind: TokenWord, Text: "TEMPLATE"},
		{Kind: TokenWord, Text: "dev"}, {Kind: TokenSymbol, Text: "."}, {Kind: TokenIdentifier, Text: `Odd"Schema`}, {Kind: TokenSymbol, Text: "."}, {Kind: TokenWord, Text: "t$1"},
		{Kind: TokenWord, Text: "FOR"}, {Kind: TokenWord, Text: "COPY"}, {Kind: TokenWord, Text: "AS"}, {Kind: TokenWord, Text: "FORMAT"}, {Kind: TokenWord, Text: "AS"}, {Kind: TokenWord, Text: "JSON"},
		{Kind: TokenString, Text: "auto", Raw: `'auto'`}, {Kind: TokenWord, Text: "NULL"}, {Kind: TokenString, Text: "", Raw: `''`}, {Kind: TokenWord, Text: "DELIMITER"}, {Kind: TokenString, Text: `it's \ ' \t`, Raw: `'it''s \\ \' \t'`},
		{Kind: TokenWord, Text: "MAXERROR"}, {Kind: TokenWord, Text: "100"}, {Kind: TokenSymbol, Text: ";"},
	}, tokens)
	// Literal and Tokens round-trip any value.
	for _, value := range []string{"", "plain", `it's`, `C:\path\`, `\t`, "multi\nline"} {
		tokens, err := Tokens(Literal(value))
		require.NoError(t, err)
		assert.Equal(t, []Token{{Kind: TokenString, Text: value, Raw: Literal(value)}}, tokens, value)
	}
	for _, text := range []string{"'open", `"open`, "/* open", "$$body", "x = :name"} {
		_, err := Tokens(text)
		require.Error(t, err, text)
	}
}

func TestTokensRetainRawLiteralEscapes(t *testing.T) {
	tokens, err := Tokens(`'\t' '\\t' '\n' '\\n'`)
	require.NoError(t, err)
	require.Len(t, tokens, 4)
	assert.Equal(t, tokens[0].Text, tokens[1].Text, "generic decoding deliberately preserves unknown command-specific escapes")
	assert.Equal(t, tokens[2].Text, tokens[3].Text)
	assert.Equal(t, `'\t'`, tokens[0].Raw)
	assert.Equal(t, `'\\t'`, tokens[1].Raw)
	assert.Equal(t, `'\n'`, tokens[2].Raw)
	assert.Equal(t, `'\\n'`, tokens[3].Raw)
}
