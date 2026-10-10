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

// TestTypeName canonicalizes Redshift aliases, in any case, to the uppercase format_type() spelling.
func TestTypeName(t *testing.T) {
	for value, expected := range map[string]Keyword{
		"int2":                            "SMALLINT",
		"int":                             "INTEGER",
		"INT4":                            "INTEGER",
		"int8":                            "BIGINT",
		"decimal":                         "NUMERIC",
		"DECIMAL(10)":                     "NUMERIC(10,0)",
		"numeric( 18 , 4 )":               "NUMERIC(18,4)",
		"numeric(38,38)":                  "NUMERIC(38,38)",
		"float4":                          "REAL",
		"float":                           "DOUBLE PRECISION",
		"FLOAT8":                          "DOUBLE PRECISION",
		"double   precision":              "DOUBLE PRECISION",
		"bool":                            "BOOLEAN",
		"char":                            "CHARACTER",
		"CHAR(10)":                        "CHARACTER(10)",
		"nchar(4096)":                     "CHARACTER(4096)",
		"bpchar":                          "CHARACTER",
		"bpchar(max)":                     "CHARACTER(4096)",
		"varchar":                         "CHARACTER VARYING",
		"VARCHAR(256)":                    "CHARACTER VARYING(256)",
		"varchar(MAX)":                    "CHARACTER VARYING(65535)",
		"nvarchar (12)":                   "CHARACTER VARYING(12)",
		"text":                            "CHARACTER VARYING",
		"Character Varying(5)":            "CHARACTER VARYING(5)",
		"date":                            "DATE",
		"timestamp":                       "TIMESTAMP WITHOUT TIME ZONE",
		"timestamp\twithout\ntime zone":   "TIMESTAMP WITHOUT TIME ZONE",
		"timestamptz":                     "TIMESTAMP WITH TIME ZONE",
		"TIMESTAMP WITH TIME ZONE":        "TIMESTAMP WITH TIME ZONE",
		"time":                            "TIME WITHOUT TIME ZONE",
		"timetz":                          "TIME WITH TIME ZONE",
		"interval year to month":          "INTERVAL YEAR TO MONTH",
		"INTERVAL DAY TO SECOND":          "INTERVAL DAY TO SECOND",
		"interval day to second ( 0 )":    "INTERVAL DAY TO SECOND(0)",
		"varbyte(10)":                     "VARBYTE(10)",
		"varbinary(max)":                  "VARBYTE(1024000)",
		"binary varying(64)":              "VARBYTE(64)",
		"GEOMETRY":                        "GEOMETRY",
		"geography":                       "GEOGRAPHY",
		"hllsketch":                       "HLLSKETCH",
		"super":                           "SUPER",
		"anyelement":                      "ANYELEMENT",
		"refcursor":                       "REFCURSOR",
		"  integer  ":                     "INTEGER",
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

// TestColumnType adds the modifier Redshift gives a column declared without one, and keeps explicit modifiers.
func TestColumnType(t *testing.T) {
	for value, expected := range map[string]Keyword{
		"char":              "CHARACTER(1)",
		"NCHAR":             "CHARACTER(1)",
		"character":         "CHARACTER(1)",
		"bpchar":            "CHARACTER(256)",
		"varchar":           "CHARACTER VARYING(256)",
		"text":              "CHARACTER VARYING(256)",
		"nvarchar":          "CHARACTER VARYING(256)",
		"character varying": "CHARACTER VARYING(256)",
		"decimal":           "NUMERIC(18,0)",
		"numeric":           "NUMERIC(18,0)",
		"varbyte":           "VARBYTE(64000)",
		"varbinary":         "VARBYTE(64000)",
		"bpchar(10)":        "CHARACTER(10)",
		"varchar(MAX)":      "CHARACTER VARYING(65535)",
		"numeric(10)":       "NUMERIC(10,0)",
		"int4":              "INTEGER",
		"timestamptz":       "TIMESTAMP WITH TIME ZONE",
		"varchar(0)":        "",
		"serial":            "",
	} {
		t.Run(value, func(t *testing.T) {
			actual, err := ColumnType(value)
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

// TestCatalogType uppercases catalog types, canonicalizing known ones and keeping quoted names byte-exact.
func TestCatalogType(t *testing.T) {
	for value, expected := range map[string]string{
		"character varying(256)": "CHARACTER VARYING(256)",
		"int4":                   "INTEGER",
		" numeric(18,0) ":        "NUMERIC(18,0)",
		"string":                 "STRING",
		"array<int>":             "ARRAY<INT>",
		`"char"`:                 `"char"`,
		"":                       "",
	} {
		assert.Equal(t, expected, CatalogType(value), value)
	}
}

// TestSignature canonicalizes and joins routine argument types and names the failing argument.
func TestSignature(t *testing.T) {
	signature, err := Signature("INT4", "varchar(10)", "timestamptz")
	require.NoError(t, err)
	assert.Equal(t, Keyword("INTEGER, CHARACTER VARYING(10), TIMESTAMP WITH TIME ZONE"), signature)
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
		// The server reads 1, the dollar string $a$--$a$, the name x$a$ and then a top-level semicolon.
		{"SELECT 1$a$--$a$ AS x$a$; DROP TABLE t --$a$", "without ';'"},
		{"1$a$--$a$x$a$;$a$", "without ';'"},
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
