package sqlclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOneOf returns the allowlisted spelling and rejects everything else.
func TestOneOf(t *testing.T) {
	for _, test := range []struct {
		value    string
		expected Keyword
	}{
		{"SELECT", "SELECT"},
		{"select", "SELECT"},
		{"Usage", "USAGE"},
	} {
		keyword, err := OneOf(test.value, "SELECT", "USAGE")
		require.NoError(t, err)
		assert.Equal(t, test.expected, keyword)
	}
	for _, value := range []string{"", "SELECT; DROP TABLE x", "SELECT ", "ALL"} {
		_, err := OneOf(value, "SELECT", "USAGE")
		require.ErrorContains(t, err, "is not one of SELECT, USAGE", value)
	}
	_, err := OneOf("SELECT")
	require.Error(t, err, "an empty allowlist accepts nothing")
}

// TestTypeName canonicalizes Redshift aliases to the format_type() spelling.
func TestTypeName(t *testing.T) {
	for value, expected := range map[string]Keyword{
		"int2":                            "smallint",
		"int":                             "integer",
		"INT4":                            "integer",
		"int8":                            "bigint",
		"decimal":                         "numeric",
		"DECIMAL(10)":                     "numeric(10,0)",
		"numeric( 18 , 4 )":               "numeric(18,4)",
		"numeric(38,38)":                  "numeric(38,38)",
		"float4":                          "real",
		"float":                           "double precision",
		"FLOAT8":                          "double precision",
		"double   precision":              "double precision",
		"bool":                            "boolean",
		"char":                            "character",
		"CHAR(10)":                        "character(10)",
		"nchar(4096)":                     "character(4096)",
		"bpchar(max)":                     "character(4096)",
		"varchar":                         "character varying",
		"VARCHAR(256)":                    "character varying(256)",
		"varchar(MAX)":                    "character varying(65535)",
		"nvarchar (12)":                   "character varying(12)",
		"text":                            "character varying",
		"Character Varying(5)":            "character varying(5)",
		"date":                            "date",
		"timestamp":                       "timestamp without time zone",
		"timestamp\twithout\ntime zone":   "timestamp without time zone",
		"timestamptz":                     "timestamp with time zone",
		"TIMESTAMP WITH TIME ZONE":        "timestamp with time zone",
		"time":                            "time without time zone",
		"timetz":                          "time with time zone",
		"interval year to month":          "interval year to month",
		"INTERVAL DAY TO SECOND":          "interval day to second",
		"interval day to second ( 0 )":    "interval day to second(0)",
		"varbyte(10)":                     "varbyte(10)",
		"varbinary(max)":                  "varbyte(1024000)",
		"binary varying(64)":              "varbyte(64)",
		"GEOMETRY":                        "geometry",
		"geography":                       "geography",
		"hllsketch":                       "hllsketch",
		"super":                           "super",
		"anyelement":                      "anyelement",
		"refcursor":                       "refcursor",
		"  integer  ":                     "integer",
		"varchar(00010)":                  "",
		"varchar(-1)":                     "",
		"varchar(+1)":                     "",
		"varchar(0)":                      "",
		"varchar(65536)":                  "",
		"char(4097)":                      "",
		"varbyte(1024001)":                "",
		"varchar(9999999999999999999999)": "",
		"varchar(1.5)":                    "",
		"numeric(0)":                      "",
		"numeric(39)":                     "",
		"numeric(5,6)":                    "",
		"numeric(10,2,1)":                 "",
		"numeric(max)":                    "",
		"interval day to second(7)":       "",
		"interval year to month(1)":       "",
		"integer(4)":                      "",
		"timestamp(3)":                    "",
		"date()":                          "",
		"varchar(10":                      "",
		"varchar10)":                      "",
		"varchar((10))":                   "",
		"varchar(10)(2)":                  "",
		"varchar(10) primary key":         "",
		"integer; DROP TABLE x":           "",
		"":                                "",
		"int4[]":                          "",
		`"integer"`:                       "",
		"interval":                        "",
		"double":                          "",
		"serial":                          "",
		"json":                            "",
	} {
		t.Run(value, func(t *testing.T) {
			actual, err := TypeName(value)
			if expected == "" {
				require.Error(t, err)
				assert.Empty(t, actual)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
}

// TestSignature canonicalizes and joins routine argument types and names the failing argument.
func TestSignature(t *testing.T) {
	signature, err := Signature("INT4", "varchar(10)", "timestamptz")
	require.NoError(t, err)
	assert.Equal(t, Keyword("integer, character varying(10), timestamp with time zone"), signature)
	signature, err = Signature()
	require.NoError(t, err)
	assert.Empty(t, signature)
	_, err = Signature("int", "nope")
	require.ErrorContains(t, err, "argument 2")
}

// TestCheckUserSQL accepts single expressions and rejects text that could end or corrupt the enclosing statement.
func TestCheckUserSQL(t *testing.T) {
	for _, text := range []string{
		"SELECT 1",
		"SELECT ';' AS semicolon, \"a;b\" FROM t",
		"SELECT 1 -- trailing; comment",
		"SELECT 1 /* a; /* nested; */ b; */",
		"SELECT $$;$$, $tag$ ; $$ $tag$",
		"SELECT E'\\';', 'it''s'",
		"(a > 1 OR b < 2) AND c = 'x)'",
		"SELECT x::text",
		"SELECT price$usd FROM t",
		"SELECT ':p', \":p\"",
		"current_date - 1",
		"SELECT '\\\\'",
	} {
		t.Run(text, func(t *testing.T) {
			checked, err := CheckUserSQL(text)
			require.NoError(t, err)
			assert.Equal(t, UserSQL(text), checked)
		})
	}
	for _, test := range []struct {
		text, message string
	}{
		{"", "empty"},
		{" \n\t", "empty"},
		{"SELECT 1;", "without ';'"},
		{"SELECT 1; DROP TABLE t", "without ';'"},
		{"SELECT (1;2)", "without ';'"},
		{"SELECT 'open", "unterminated string literal"},
		{`SELECT "open`, "unterminated quoted identifier"},
		{"SELECT 1 /* open", "unterminated block comment"},
		{"SELECT 1 /* a /* b */", "unterminated block comment"},
		{"SELECT $$open", "unterminated dollar-quoted string"},
		{"SELECT $a$ x $b$", "unterminated dollar-quoted string"},
		{"SELECT (1", "unmatched '('"},
		{"SELECT 1) UNION (SELECT 2", "unmatched ')'"},
		// Redshift reads x$a$ as a name, so the semicolon is outside any dollar quote there.
		{"SELECT x$a$; DROP TABLE t; $a$", "without ';'"},
		{"SELECT é$a$; DROP TABLE t; $a$", "without ';'"},
		{"SELECT a FROM t WHERE b = :b", "placeholder :b"},
		{"1) OR (true", "unmatched ')'"},
		// Redshift reads \' as an escaped quote, so the semicolon below is outside the literal there.
		{`SELECT '\'' ; DROP TABLE t; SELECT '''''`, "without ';'"},
		// The transport reads the backslash literally, so the literal ends early and leaves a semicolon outside.
		{`SELECT 'a\'; DROP TABLE t; --'`, "without ';'"},
		{`SELECT '\'`, "unterminated string literal"},
	} {
		t.Run(test.text, func(t *testing.T) {
			_, err := CheckUserSQL(test.text)
			require.ErrorContains(t, err, test.message)
		})
	}
}
