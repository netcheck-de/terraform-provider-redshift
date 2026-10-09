package sqlclient

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatementRendering covers every builder method, including skipped optional forms and hostile values.
func TestStatementRendering(t *testing.T) {
	limit, zero := int64(10), int64(0)
	yes, no := true, false
	for _, test := range []struct {
		name      string
		statement Statement
		expected  string
	}{
		{"verb", Stmt("DROP USER").Ident("u"), `DROP USER "u"`},
		{"multi-word verb", Stmt("ALTER", "DEFAULT PRIVILEGES"), "ALTER DEFAULT PRIVILEGES"},
		{"empty fragment", Fragment(), ""},
		{"empty keywords skipped", Stmt("SELECT", "", "1"), "SELECT 1"},
		{"identifier quote", Stmt("DROP ROLE").Ident(`a"; DROP USER x; --`), `DROP ROLE "a""; DROP USER x; --"`},
		{"empty identifier", Stmt("DROP ROLE").Ident(""), `DROP ROLE ""`},
		{"qualified", Stmt("DROP TABLE").Qualified("s", `t"x`), `DROP TABLE "s"."t""x"`},
		{"qualified optional schema", Stmt("DROP TABLE").Qualified("", "t"), `DROP TABLE "t"`},
		{"qualified three parts", Stmt("COMMENT ON COLUMN").Qualified("s", "t", "c"), `COMMENT ON COLUMN "s"."t"."c"`},
		{"qualified empty last part", Stmt("DROP TABLE").Qualified("s", ""), `DROP TABLE "s".""`},
		{"literal quote and backslash", Stmt("SELECT").Lit(`a\'; DROP USER x; --`), `SELECT 'a\\''; DROP USER x; --'`},
		{"empty literal", Stmt("SELECT").Lit(""), "SELECT ''"},
		{"int", Stmt("SELECT").Int(-42).Int(math.MaxInt64), "SELECT -42 9223372036854775807"},
		{"bool", Stmt("SELECT").Bool(true).Bool(false), "SELECT true false"},
		{"json", Stmt("CREATE IDENTITY PROVIDER").Ident("idp").KwLit("TYPE", "azure").Kw("PARAMETERS").JSON(map[string]any{"issuer": "https://x/?a=<b>&c", "audience": []string{"api://o'k"}}), `CREATE IDENTITY PROVIDER "idp" TYPE 'azure' PARAMETERS '{"audience":["api://o''k"],"issuer":"https://x/?a=<b>&c"}'`},
		{"json escapes", Stmt("SELECT").JSON(`a"\`), `SELECT '"a\\"\\\\"'`},
		{"body", Stmt("AS").Body("SELECT 1"), "AS $$SELECT 1$$"},
		{"body containing $$", Stmt("AS").Body("SELECT $$x$$"), "AS $body$SELECT $$x$$$body$"},
		{"body ending in $", Stmt("AS").Body("SELECT 'x$'||'$"), "AS $body$SELECT 'x$'||'$$body$"},
		{"body containing both tags", Stmt("AS").Body("$$ $body$"), "AS $body1$$$ $body$$body1$"},
		{"body ending in partial tag", Stmt("AS").Body("$$ $body"), "AS $body1$$$ $body$body1$"},
		{"empty body", Stmt("AS").Body(""), "AS $$$$"},
		{"verbatim", Stmt("CREATE VIEW").Ident("v").Kw("AS").Verbatim("SELECT 1").Kw("WITH NO SCHEMA BINDING"), `CREATE VIEW "v" AS SELECT 1 WITH NO SCHEMA BINDING`},
		{"verbatim trailing comment", Stmt("CREATE VIEW").Ident("v").Kw("AS").Verbatim("SELECT 1 -- note").Kw("WITH NO SCHEMA BINDING"), "CREATE VIEW \"v\" AS SELECT 1 -- note\n WITH NO SCHEMA BINDING"},
		{"verbatim closed comment", Stmt("SELECT").Verbatim("1 -- note\n").Int(2), "SELECT 1 -- note\n 2"},
		{"verbatim comment only under Redshift escapes", Stmt("SELECT").Verbatim(`'\' -- '`).Int(2), "SELECT '\\' -- '\n 2"},
		{"verbatim quoted dashes", Stmt("SELECT").Verbatim("'--'").Int(2), "SELECT '--' 2"},
		{"empty verbatim", Stmt("SELECT").Verbatim("").Int(1), "SELECT 1"},
		{"append", Stmt("GRANT SELECT ON TABLE").Ident("t").Kw("TO").Append(Fragment().KwIdent("ROLE", "r"), Fragment(), Kw("PUBLIC")), `GRANT SELECT ON TABLE "t" TO ROLE "r" PUBLIC`},
		{"kw ident", Stmt("ALTER SCHEMA").Ident("s").KwIdent("OWNER TO", "o"), `ALTER SCHEMA "s" OWNER TO "o"`},
		{"kw lit", Stmt("ALTER USER").Ident("u").KwLit("PASSWORD", "p"), `ALTER USER "u" PASSWORD 'p'`},
		{"kw int", Stmt("ALTER USER").Ident("u").KwInt("CONNECTION LIMIT", 5), `ALTER USER "u" CONNECTION LIMIT 5`},
		{"kw qualified", Stmt("GRANT SELECT").KwQualified("ON TABLE", "s", "t"), `GRANT SELECT ON TABLE "s"."t"`},
		{"opt ident set", Stmt("DROP").OptIdent("IN SCHEMA", "s"), `DROP IN SCHEMA "s"`},
		{"opt ident empty", Stmt("DROP").OptIdent("IN SCHEMA", ""), "DROP"},
		{"opt lit set", Stmt("CREATE").OptLit("REGION", "eu-central-1"), "CREATE REGION 'eu-central-1'"},
		{"opt lit empty", Stmt("CREATE").OptLit("REGION", ""), "CREATE"},
		{"opt kw set", Stmt("CREATE").OptKw("TEMP"), "CREATE TEMP"},
		{"opt kw empty", Stmt("CREATE").OptKw(""), "CREATE"},
		{"opt int set", Stmt("ALTER USER").Ident("u").OptInt("CONNECTION LIMIT", &limit), `ALTER USER "u" CONNECTION LIMIT 10`},
		{"opt int zero", Stmt("ALTER USER").Ident("u").OptInt("CONNECTION LIMIT", &zero), `ALTER USER "u" CONNECTION LIMIT 0`},
		{"opt int nil", Stmt("ALTER USER").Ident("u").OptInt("CONNECTION LIMIT", nil), `ALTER USER "u"`},
		{"opt toggle true", Stmt("ALTER USER").OptToggle(&yes, "CREATEDB", "NOCREATEDB"), "ALTER USER CREATEDB"},
		{"opt toggle false", Stmt("ALTER USER").OptToggle(&no, "CREATEDB", "NOCREATEDB"), "ALTER USER NOCREATEDB"},
		{"opt toggle nil", Stmt("ALTER USER").OptToggle(nil, "CREATEDB", "NOCREATEDB"), "ALTER USER"},
		{"if true", Stmt("CREATE DATABASE").If(true, "WITH", "PERMISSIONS"), "CREATE DATABASE WITH PERMISSIONS"},
		{"if false", Stmt("CREATE DATABASE").If(false, "WITH PERMISSIONS"), "CREATE DATABASE"},
		{"toggle true", Stmt("ALTER DATASHARE").Toggle(true, "ON", "OFF"), "ALTER DATASHARE ON"},
		{"toggle false", Stmt("ALTER DATASHARE").Toggle(false, "ON", "OFF"), "ALTER DATASHARE OFF"},
		{"when true", Stmt("CREATE").When(true, func(s Statement) Statement { return s.Kw("A").Ident("b") }), `CREATE A "b"`},
		{"when false", Stmt("CREATE").When(false, func(s Statement) Statement { return s.Kw("A") }), "CREATE"},
		{"list", Stmt("GRANT").List(Kw("SELECT"), Fragment(), Kw("INSERT")).Kw("ON TABLE"), "GRANT SELECT, INSERT ON TABLE"},
		{"empty list", Stmt("GRANT").List().Kw("ON"), "GRANT ON"},
		{"paren", Stmt("CREATE TABLE").Ident("t").Paren(Ident("a").Kw("INTEGER"), Ident("b").Kw("VARCHAR(10)")), `CREATE TABLE "t" ("a" INTEGER, "b" VARCHAR(10))`},
		{"empty paren", Stmt("SELECT").Kw("f").Paren(), "SELECT f ()"},
		{"args", Stmt("CREATE TABLE").Paren(Ident("id").Kw("BIGINT IDENTITY").Args(Int(1), Int(1))), `CREATE TABLE ("id" BIGINT IDENTITY(1, 1))`},
		{"args without previous token", Fragment().Args(Lit("x")), "('x')"},
		{"empty args", Stmt("DROP FUNCTION").Qualified("s", "f").Args(), `DROP FUNCTION "s"."f"()`},
		{"opt paren set", Stmt("DISTSTYLE").OptParen(Fragment(), Ident("a")), `DISTSTYLE ("a")`},
		{"opt paren empty", Stmt("DISTSTYLE").OptParen(Fragment()), "DISTSTYLE"},
		{"opt args set", Stmt("SELECT").Kw("f").OptArgs(Int(1)), "SELECT f(1)"},
		{"opt args empty", Stmt("SELECT").Kw("f").OptArgs(), "SELECT f"},
		{"idents", Stmt("SORTKEY").Paren(Fragment().Idents("a", `b"c`)), `SORTKEY ("a", "b""c")`},
		{"no idents", Stmt("SORTKEY").Idents(), "SORTKEY"},
		{"shorthands", Fragment().List(Kw("A", "B"), Ident("i"), Lit("l"), Int(3)), `A B, "i", 'l', 3`},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.statement.Err())
			assert.Equal(t, test.expected, test.statement.String())
		})
	}
}

// TestStatementByteForByte reproduces statements that existing resources send, so migrated renderers stay identical.
func TestStatementByteForByte(t *testing.T) {
	recipient := Fragment().KwIdent("ROLE", "r")
	externalSchema := func(region string) string {
		return Stmt("CREATE EXTERNAL SCHEMA").Ident("spectrum").KwLit("FROM DATA CATALOG DATABASE", "glue_db").KwLit("IAM_ROLE", "arn:aws:iam::123456789012:role/spectrum").OptLit("REGION", region).String()
	}
	for _, test := range []struct {
		name, actual, expected string
	}{
		{
			"create user",
			Stmt("CREATE USER").Ident("grafana").KwLit("PASSWORD", "s3cret").Toggle(false, "CREATEUSER", "NOCREATEUSER").Toggle(false, "CREATEDB", "NOCREATEDB").String(),
			`CREATE USER "grafana" PASSWORD 's3cret' NOCREATEUSER NOCREATEDB`,
		},
		{
			"shared database",
			Stmt("CREATE DATABASE").Ident("sales_consumer").If(true, "WITH PERMISSIONS").KwIdent("FROM DATASHARE", "sales").KwLit("OF ACCOUNT", "123456789012").KwLit("NAMESPACE", "a1b2c3d4-5678-90ab-cdef-111111111111").String(),
			`CREATE DATABASE "sales_consumer" WITH PERMISSIONS FROM DATASHARE "sales" OF ACCOUNT '123456789012' NAMESPACE 'a1b2c3d4-5678-90ab-cdef-111111111111'`,
		},
		{
			"external schema with region",
			externalSchema("us-east-1"),
			`CREATE EXTERNAL SCHEMA "spectrum" FROM DATA CATALOG DATABASE 'glue_db' IAM_ROLE 'arn:aws:iam::123456789012:role/spectrum' REGION 'us-east-1'`,
		},
		{
			"external schema without region",
			externalSchema(""),
			`CREATE EXTERNAL SCHEMA "spectrum" FROM DATA CATALOG DATABASE 'glue_db' IAM_ROLE 'arn:aws:iam::123456789012:role/spectrum'`,
		},
		{
			"default privileges",
			Stmt("ALTER DEFAULT PRIVILEGES").KwIdent("FOR USER", "etl").OptIdent("IN SCHEMA", "s").Kw("GRANT").List(Kw("SELECT")).Kw("ON TABLES", "TO").Append(recipient).String(),
			`ALTER DEFAULT PRIVILEGES FOR USER "etl" IN SCHEMA "s" GRANT SELECT ON TABLES TO ROLE "r"`,
		},
		{
			"assumerole default",
			Stmt("GRANT ASSUMEROLE ON").Kw("default").Kw("TO").Append(recipient).Kw("FOR", "COPY").String(),
			`GRANT ASSUMEROLE ON default TO ROLE "r" FOR COPY`,
		},
		{
			"assumerole arn",
			Stmt("REVOKE ASSUMEROLE ON").Lit("arn:aws:iam::123456789012:role/copy").Kw("FROM").Append(recipient).Kw("FOR", "EXTERNAL FUNCTION").String(),
			`REVOKE ASSUMEROLE ON 'arn:aws:iam::123456789012:role/copy' FROM ROLE "r" FOR EXTERNAL FUNCTION`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.actual)
		})
	}
}

// TestStatementValueSemantics checks that derived statements never change a shared prefix or each other.
func TestStatementValueSemantics(t *testing.T) {
	alter := Stmt("ALTER USER").Ident("u")
	// Spare capacity in the prefix would let sibling appends overwrite each other's tokens.
	prefix := Statement{tokens: make([]string, 0, 16)}.Kw("ALTER USER").Ident("u")
	first, second := prefix.Kw("CREATEDB"), prefix.Kw("NOCREATEDB")
	password := alter.KwLit("PASSWORD", "p")
	limit := alter.KwInt("CONNECTION LIMIT", 1)
	assert.Equal(t, `ALTER USER "u"`, alter.String())
	assert.Equal(t, `ALTER USER "u" PASSWORD 'p'`, password.String())
	assert.Equal(t, `ALTER USER "u" CONNECTION LIMIT 1`, limit.String())
	assert.Equal(t, `ALTER USER "u" CREATEDB`, first.String())
	assert.Equal(t, `ALTER USER "u" NOCREATEDB`, second.String())
	assert.Equal(t, `ALTER USER "u"`, prefix.String())

	// Args rewrites the previous token, which must not leak into the receiver or into a sibling.
	function := Stmt("DROP FUNCTION").Qualified("s", "f")
	withArgs := function.Args(Kw("integer"))
	other := function.Args(Kw("bigint"))
	assert.Equal(t, `DROP FUNCTION "s"."f"`, function.String())
	assert.Equal(t, `DROP FUNCTION "s"."f"(integer)`, withArgs.String())
	assert.Equal(t, `DROP FUNCTION "s"."f"(bigint)`, other.String())

	// Appending a fragment copies its tokens, so later use of the fragment is independent.
	fragment := Fragment().KwIdent("ROLE", "r")
	grant := Stmt("GRANT").Append(fragment)
	_ = grant.Args(Lit("x"))
	assert.Equal(t, `ROLE "r"`, fragment.String())
	assert.Equal(t, `GRANT ROLE "r"`, grant.String())
}

// TestStatementErrors checks that a JSON failure travels with every derived statement and list.
func TestStatementErrors(t *testing.T) {
	broken := Fragment().JSON(func() {})
	require.Error(t, broken.Err())
	assert.Empty(t, broken.String())
	for name, statement := range map[string]Statement{
		"derived":   Stmt("CREATE").JSON(math.NaN()).Kw("X"),
		"append":    Stmt("CREATE").Append(broken),
		"list":      Stmt("CREATE").List(broken),
		"paren":     Stmt("CREATE").Paren(broken),
		"args":      Stmt("CREATE").Args(broken),
		"opt paren": Stmt("CREATE").OptParen(broken),
		"opt args":  Stmt("CREATE").OptArgs(broken),
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, statement.Err(), "encode JSON option")
		})
	}
	first := Stmt("CREATE").JSON(math.Inf(1)).JSON(func() {})
	require.ErrorContains(t, first.Err(), "+Inf")
}
