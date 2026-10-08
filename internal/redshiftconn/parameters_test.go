package redshiftconn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBindParametersPreservesSQLLexicalBoundaries covers quoting, casts, nested comments, and dollar-quoted bodies.
func TestBindParametersPreservesSQLLexicalBoundaries(t *testing.T) {
	for _, test := range []struct {
		sql, expected string
		parameters    map[string]string
		args          []any
	}{
		{"SELECT :name, :name, :other", "SELECT $1, $1, $2", map[string]string{"name": "O'Reilly; DROP TABLE x", "other": "two"}, []any{"O'Reilly; DROP TABLE x", "two"}},
		{`SELECT ':ignored', "a:ignored", 'it''s :ignored', "a""b", :p::text`, `SELECT ':ignored', "a:ignored", 'it''s :ignored', "a""b", $1::text`, map[string]string{"p": "value"}, []any{"value"}},
		{"SELECT :p -- :ignored\n, :p /* :ignored /* nested */ :ignored */", "SELECT $1 -- :ignored\n, $1 /* :ignored /* nested */ :ignored */", map[string]string{"p": "value"}, []any{"value"}},
		{`SELECT $$:ignored$$, $body$:ignored$body$, :p`, `SELECT $$:ignored$$, $body$:ignored$body$, $1`, map[string]string{"p": "value"}, []any{"value"}},
		{`SELECT E'escaped\':ignored', :p`, `SELECT E'escaped\':ignored', $1`, map[string]string{"p": "value"}, []any{"value"}},
		{`SELECT '\', :p`, `SELECT '\', $1`, map[string]string{"p": "value"}, []any{"value"}},
		{"-- :ignored", "-- :ignored", nil, nil},
		{"SELECT /* unfinished :ignored", "SELECT /* unfinished :ignored", nil, nil},
		{"SELECT 'unfinished :ignored", "SELECT 'unfinished :ignored", nil, nil},
		{"SELECT $tag$:ignored", "SELECT $tag$:ignored", nil, nil},
		{"SELECT $1, :=, :9, :", "SELECT $1, :=, :9, :", nil, nil},
		{"SELECT :_x1", "SELECT $1", map[string]string{"_x1": "value"}, []any{"value"}},
	} {
		t.Run(test.sql, func(t *testing.T) {
			statement, args, err := bindParameters(test.sql, test.parameters)
			require.NoError(t, err)
			assert.Equal(t, test.expected, statement)
			assert.Equal(t, test.args, args)
		})
	}
}

// TestBindParametersRejectsMissingAndUnusedValues fails before opening a connection for inconsistent bindings.
func TestBindParametersRejectsMissingAndUnusedValues(t *testing.T) {
	_, _, err := bindParameters("SELECT :missing", nil)
	require.ErrorContains(t, err, "missing SQL parameter")
	_, _, err = bindParameters("SELECT 1", map[string]string{"unused": "value"})
	require.ErrorContains(t, err, "unused SQL parameter")
}
