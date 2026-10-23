package sqlclient

import (
	"fmt"
	"strings"
)

// TokenKind classifies one token of SQL text that the server printed, such as SHOW TEMPLATE output.
type TokenKind int

const (
	// TokenWord is an unquoted keyword, name, or number, as written.
	TokenWord TokenKind = iota
	// TokenString is a string literal; Text holds its decoded value.
	TokenString
	// TokenIdentifier is a double-quoted identifier; Text holds its decoded name.
	TokenIdentifier
	// TokenSymbol is one punctuation character outside quotes, such as . , ; ( or ).
	TokenSymbol
)

// Token is one lexical unit of SQL text.
type Token struct {
	// Kind selects how Text is to be read.
	Kind TokenKind
	// Text is the word or symbol as written, or the decoded value of a literal or quoted identifier.
	Text string
	// Raw retains the quoted spelling of string literals for command-specific escape decoding.
	// Other token kinds leave it empty; Text keeps its existing transport-neutral decoding.
	Raw string
}

// Tokens splits SQL text into words, decoded string literals and quoted identifiers, and punctuation, using the
// lexer's Redshift reading, so catalog output such as a SHOW statement can be parsed without regular expressions.
// Comments are dropped. Unterminated quotes and block comments, dollar-quoted text, and :name placeholders are errors,
// because a statement the server printed never contains them.
func Tokens(sql string) ([]Token, error) {
	var tokens []Token
	for _, item := range lex(sql, redshiftReading) {
		if item.open && item.kind != lexLineComment {
			return nil, fmt.Errorf("SQL text has an unterminated %s", lexemeNames[item.kind])
		}
		switch item.kind {
		case lexString:
			tokens = append(tokens, Token{Kind: TokenString, Text: decodeLiteral(item.text), Raw: item.text})
		case lexQuotedIdentifier:
			name := item.text[1 : len(item.text)-1]
			tokens = append(tokens, Token{Kind: TokenIdentifier, Text: strings.ReplaceAll(name, `""`, `"`)})
		case lexLineComment, lexBlockComment:
		case lexDollarQuote, lexParameter:
			return nil, fmt.Errorf("SQL text has unexpected %q", item.text)
		default:
			tokens = append(tokens, codeTokens(item.text)...)
		}
	}
	return tokens, nil
}

// decodeLiteral reverses Literal: the enclosing quotes are removed, a doubled quote and a backslash-escaped quote
// become one quote, and a doubled backslash one backslash. Other backslash sequences are kept as written, because
// their meaning depends on the command that reads the value, such as a COPY DELIMITER '\t'.
func decodeLiteral(text string) string {
	body := text[1 : len(text)-1]
	var value strings.Builder
	for i := 0; i < len(body); i++ {
		switch {
		case body[i] == '\'' && i+1 < len(body) && body[i+1] == '\'':
			value.WriteByte('\'')
			i++
		case body[i] == '\\' && i+1 < len(body) && (body[i+1] == '\\' || body[i+1] == '\''):
			value.WriteByte(body[i+1])
			i++
		default:
			value.WriteByte(body[i])
		}
	}
	return value.String()
}

// codeTokens splits unquoted text at white space and punctuation. Letters, digits, _, $, and multibyte characters
// continue a word, as they continue an unquoted Redshift name.
func codeTokens(text string) []Token {
	var tokens []Token
	start := -1
	flush := func(end int) {
		if start >= 0 {
			tokens = append(tokens, Token{Kind: TokenWord, Text: text[start:end]})
			start = -1
		}
	}
	for i := range len(text) {
		char := text[i]
		switch {
		case identifierByte(char, false) || char == '$' || char >= 0x80:
			if start < 0 {
				start = i
			}
		case char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f' || char == '\v':
			flush(i)
		default:
			flush(i)
			tokens = append(tokens, Token{Kind: TokenSymbol, Text: string(char)})
		}
	}
	flush(len(text))
	return tokens
}
