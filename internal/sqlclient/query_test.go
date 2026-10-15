package sqlclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueryBuild covers every clause and the reproduction of existing catalog queries.
func TestQueryBuild(t *testing.T) {
	for _, test := range []struct {
		name   string
		query  Query
		sql    string
		params map[string]string
	}{
		{
			name:  "no binds",
			query: Select("current_user"),
			sql:   "SELECT current_user",
		},
		{
			name:   "local database",
			query:  Select("database_name").From("svv_redshift_databases").Where("database_name = :database", Bind("database", "analytics")).Where("database_type = 'local'"),
			sql:    "SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local'",
			params: map[string]string{"database": "analytics"},
		},
		{
			name: "default privileges with schema",
			query: Select("privilege_type", "admin_option").From("svv_default_privileges").Where("owner_name = :owner", Bind("owner", "etl")).
				WhereEither(true, "schema_name = :schema", "(schema_name IS NULL OR schema_name = '')", Bind("schema", "s")).
				Where("object_type = :object_type", Bind("object_type", "RELATION")).Where("grantee_name = :grantee", Bind("grantee", "r")).Where("grantee_type = LOWER(:kind)", Bind("kind", "ROLE")),
			sql:    "SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind)",
			params: map[string]string{"owner": "etl", "schema": "s", "object_type": "RELATION", "grantee": "r", "kind": "ROLE"},
		},
		{
			name: "default privileges without schema",
			query: Select("privilege_type").From("svv_default_privileges").Where("owner_name = :owner", Bind("owner", "etl")).
				WhereEither(false, "schema_name = :schema", "(schema_name IS NULL OR schema_name = '')", Bind("schema", "s")),
			sql:    "SELECT privilege_type FROM svv_default_privileges WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '')",
			params: map[string]string{"owner": "etl"},
		},
		{
			name:   "where if",
			query:  Select("a").From("t").WhereIf(true, "a = :a", Bind("a", "1")).WhereIf(false, "b = :b", Bind("b", "2")),
			sql:    "SELECT a FROM t WHERE a = :a",
			params: map[string]string{"a": "1"},
		},
		{
			name:   "opt eq",
			query:  Select("schema_name").From("svv_all_schemas").OptEq("database_name", "database", "db").OptEq("schema_name", "schema", ""),
			sql:    "SELECT schema_name FROM svv_all_schemas WHERE database_name = :database",
			params: map[string]string{"database": "db"},
		},
		{
			name:  "order by",
			query: Select("a", "b").From("t").OrderBy("a").OrderBy("b DESC"),
			sql:   "SELECT a, b FROM t ORDER BY a, b DESC",
		},
		{
			name:   "from binds",
			query:  Select("x").From("(SELECT :v AS x) q", Bind("v", "1")),
			sql:    "SELECT x FROM (SELECT :v AS x) q",
			params: map[string]string{"v": "1"},
		},
		{
			name:   "repeated placeholder and identical bind",
			query:  Select("a").From("t").Where("a = :n", Bind("n", "x")).Where("b = :n", Bind("n", "x")),
			sql:    "SELECT a FROM t WHERE a = :n AND b = :n",
			params: map[string]string{"n": "x"},
		},
		{
			name:   "casts, quotes and comments are not placeholders",
			query:  Select("a::text").From("t").Where("a = ':nope'").Where(`"b:c" = :v /* :nope */`, Bind("v", "x")),
			sql:    `SELECT a::text FROM t WHERE a = ':nope' AND "b:c" = :v /* :nope */`,
			params: map[string]string{"v": "x"},
		},
		{
			name:  "single condition may use OR",
			query: Select("a").From("t").Where("a = 1 OR b = 2"),
			sql:   "SELECT a FROM t WHERE a = 1 OR b = 2",
		},
		{
			name:  "parenthesized or quoted OR",
			query: Select("a").From("t").Where("(a = 1 OR b = 2)").Where("c = 'x OR y'").Where("color = 1").Where(`"or" = 1`),
			sql:   `SELECT a FROM t WHERE (a = 1 OR b = 2) AND c = 'x OR y' AND color = 1 AND "or" = 1`,
		},
		{
			name:   "hostile value stays a parameter",
			query:  Select("usename").From("pg_user").Where("usename = :name", Bind("name", `x' OR '1'='1`)),
			sql:    "SELECT usename FROM pg_user WHERE usename = :name",
			params: map[string]string{"name": `x' OR '1'='1`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql, params, err := test.query.Build()
			require.NoError(t, err)
			assert.Equal(t, test.sql, sql)
			assert.Equal(t, test.params, params)
			if test.params == nil {
				assert.Nil(t, params, "queries without placeholders pass nil parameters")
			}
		})
	}
}

// TestQueryBuildRejectsInconsistentBinds fails before execution when placeholders and parameters diverge.
func TestQueryBuildRejectsInconsistentBinds(t *testing.T) {
	for _, test := range []struct {
		name, message string
		query         Query
	}{
		{"no columns", "no select list", Select().From("t")},
		{"missing", "placeholder :a has no binding", Select("a").From("t").Where("a = :a")},
		{"unused", "parameter :b is not used", Select("a").From("t").Where("a = :a", Bind("a", "1"), Bind("b", "2"))},
		{"unused in from", "parameter :b is not used", Select("a").From("t", Bind("b", "2"))},
		{"conflicting", "bound to different values", Select("a").From("t").Where("a = :n", Bind("n", "1")).Where("b = :n", Bind("n", "2"))},
		{"empty", "parameter :a is empty", Select("a").From("t").Where("a = :a", Bind("a", ""))},
		{"placeholder only in comment", "parameter :a is not used", Select("a").From("t").Where("b = 1 -- :a\n", Bind("a", "1"))},
		{"top-level OR", "must parenthesize OR", Select("a").From("t").Where("a = 1").Where("b = 1 or c = 1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, params, err := test.query.Build()
			require.ErrorContains(t, err, test.message)
			assert.Nil(t, params)
		})
	}
}

// TestQueryValueSemantics checks that queries derived from one base stay independent.
func TestQueryValueSemantics(t *testing.T) {
	base := Select("a").From("t").Where("a = :a", Bind("a", "1"))
	left := base.Where("b = :b", Bind("b", "2")).OrderBy("a")
	right := base.Where("c = :c", Bind("c", "3"))
	for _, test := range []struct {
		query  Query
		sql    string
		params map[string]string
	}{
		{base, "SELECT a FROM t WHERE a = :a", map[string]string{"a": "1"}},
		{left, "SELECT a FROM t WHERE a = :a AND b = :b ORDER BY a", map[string]string{"a": "1", "b": "2"}},
		{right, "SELECT a FROM t WHERE a = :a AND c = :c", map[string]string{"a": "1", "c": "3"}},
	} {
		sql, params, err := test.query.Build()
		require.NoError(t, err)
		assert.Equal(t, test.sql, sql)
		assert.Equal(t, test.params, params)
	}
	columns := []Keyword{"a"}
	query := Select(columns...)
	columns[0] = "b"
	sql, _, err := query.Build()
	require.NoError(t, err)
	assert.Equal(t, "SELECT a", sql)
}
