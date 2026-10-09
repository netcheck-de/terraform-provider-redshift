package sqlclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPlaceholdersMatchTransportBinding mirrors the direct transport's binder cases, so Query.Build validates
// exactly the placeholders that will be bound.
func TestPlaceholdersMatchTransportBinding(t *testing.T) {
	for _, test := range []struct {
		sql      string
		expected []string
	}{
		{"SELECT :name, :name, :other", []string{"name", "other"}},
		{`SELECT ':ignored', "a:ignored", 'it''s :ignored', "a""b", :p::text`, []string{"p"}},
		{"SELECT :p -- :ignored\n, :p /* :ignored /* nested */ :ignored */", []string{"p"}},
		{`SELECT $$:ignored$$, $body$:ignored$body$, :p`, []string{"p"}},
		{`SELECT E'escaped\':ignored', :p`, []string{"p"}},
		{`SELECT e'escaped\':ignored', :p`, []string{"p"}},
		{`SELECT '\', :p`, []string{"p"}},
		{`SELECT CASE WHEN x THEN 'a' ELSE 'b' END, :p`, []string{"p"}},
		{"-- :ignored", nil},
		{"SELECT /* unfinished :ignored", nil},
		{"SELECT 'unfinished :ignored", nil},
		{"SELECT $tag$:ignored", nil},
		{"SELECT $1, :=, :9, :", nil},
		{"SELECT $1:p", []string{"p"}},
		{"SELECT :_x1", []string{"_x1"}},
	} {
		t.Run(test.sql, func(t *testing.T) {
			assert.Equal(t, test.expected, placeholders(test.sql))
		})
	}
}

// TestLexBoundaries checks lexeme kinds, exact spans and open constructs under both backslash readings.
func TestLexBoundaries(t *testing.T) {
	sql := "SELECT E'a\\'b', 'c\\', \"d\"\"e\" -- f\n/* g /* h */ */ $t$ i $t$ x::int :p"
	var kinds []lexemeKind
	var texts []string
	for _, item := range lex(sql, transportReading) {
		kinds = append(kinds, item.kind)
		texts = append(texts, item.text)
		assert.False(t, item.open, item.text)
	}
	assert.Equal(t, []lexemeKind{
		lexCode, lexString, lexCode, lexString, lexCode, lexQuotedIdentifier, lexCode, lexLineComment,
		lexBlockComment, lexCode, lexDollarQuote, lexCode, lexOperator, lexCode, lexParameter,
	}, kinds)
	assert.Equal(t, sql, strings.Join(texts, ""), "lexemes cover the text without gaps")
	assert.Equal(t, `'c\'`, texts[3])
	assert.Equal(t, "-- f\n", texts[7])

	// Under Redshift's reading the backslash escapes the quote, so the second literal runs to the next quote.
	redshift := lex(`'c\', 'd'`, redshiftReading)
	assert.Equal(t, []lexeme{{kind: lexString, text: `'c\', '`}, {kind: lexCode, text: "d"}, {kind: lexString, text: "'", open: true}}, redshift)

	for _, test := range []struct {
		sql  string
		kind lexemeKind
	}{
		{"'a", lexString},
		{`"a`, lexQuotedIdentifier},
		{"-- a", lexLineComment},
		{"/* a /* b */", lexBlockComment},
		{"$a$ b", lexDollarQuote},
	} {
		lexemes := lex(test.sql, transportReading)
		last := lexemes[len(lexemes)-1]
		assert.Equal(t, test.kind, last.kind, test.sql)
		assert.True(t, last.open, test.sql)
	}
	// Redshift continues a name through $, so only the transport sees a dollar quote after x; after a space,
	// a closed quote or the start of the text both readings open one.
	assert.Equal(t, []lexeme{{kind: lexCode, text: "x"}, {kind: lexDollarQuote, text: "$a$;$a$"}}, lex("x$a$;$a$", transportReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "x$a$;"}, {kind: lexDollarQuote, text: "$a$ 1", open: true}}, lex("x$a$;$a$ 1", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "é$a$"}}, lex("é$a$", redshiftReading))
	// A number or positional parameter is not a name, so the server opens a dollar quote right after it.
	assert.Equal(t, []lexeme{{kind: lexCode, text: "1"}, {kind: lexDollarQuote, text: "$a$;$a$"}}, lex("1$a$;$a$", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "x 1e5"}, {kind: lexDollarQuote, text: "$a$;$a$"}}, lex("x 1e5$a$;$a$", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "$1"}, {kind: lexDollarQuote, text: "$a$;$a$"}}, lex("$1$a$;$a$", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "x1$a$;"}}, lex("x1$a$;", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "_1$a$;"}}, lex("_1$a$;", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "x "}, {kind: lexDollarQuote, text: "$a$;$a$"}, {kind: lexDollarQuote, text: "$b$$b$"}}, lex("x $a$;$a$$b$$b$", redshiftReading))
	assert.Equal(t, []lexeme{{kind: lexDollarQuote, text: "$$;$$"}}, lex("$$;$$", redshiftReading))
	assert.Empty(t, lex("", transportReading))
	assert.Equal(t, []lexeme{{kind: lexCode, text: "$1 $ $9$"}}, lex("$1 $ $9$", transportReading), "a digit cannot start a dollar tag")
}

// TestEndsInLineComment detects a trailing comment under either backslash reading.
func TestEndsInLineComment(t *testing.T) {
	for sql, expected := range map[string]bool{
		"SELECT 1":             false,
		"SELECT 1 -- c":        true,
		"SELECT 1 -- c\n":      false,
		"SELECT '--'":          false,
		"SELECT /* -- */ 1":    false,
		`SELECT '\' -- '`:      true,
		`SELECT 'a\'' -- ''`:   true,
		"SELECT $$ -- $$":      false,
		"SELECT 1 --":          true,
		"":                     false,
		"SELECT \"--\" AS x":   false,
		"SELECT 1 -- a\n -- b": true,
	} {
		assert.Equal(t, expected, endsInLineComment(sql), sql)
	}
}
