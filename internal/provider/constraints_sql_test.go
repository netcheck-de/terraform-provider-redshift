package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConstraintsSQL pins the constraint listing with and without its filters.
func TestConstraintsSQL(t *testing.T) {
	query := func(schemaName, table, constraintType string) func() (string, error) {
		return func() (string, error) {
			query, err := readConstraintsQuery(schemaName, table, constraintType)
			if err != nil {
				return "", err
			}
			return discoverySQL(query)()
		}
	}
	checkSQL(t, "constraints", []sqlCase{
		{"all", query("", "", "")},
		{"table", query(`Odd"Schema`, "Orders", "")},
		{"foreign_keys", query("serving", "", "FOREIGN KEY")},
		{"unique", query("", "", "UNIQUE")},
		{"unknown_type", query("", "", "CHECK")},
	})
}

// TestConstraintsKeyColumns parses key columns from pg_get_constraintdef output, including quoted identifiers.
func TestConstraintsKeyColumns(t *testing.T) {
	for _, test := range []struct {
		definition string
		foreign    bool
		columns    []string
		referenced []string
	}{
		{"PRIMARY KEY (id)", false, []string{"id"}, nil},
		{"UNIQUE (b, a)", false, []string{"b", "a"}, nil},
		{`UNIQUE ("Odd""Name", "with space", plain)`, false, []string{`Odd"Name`, "with space", "plain"}, nil},
		{"FOREIGN KEY (order_id, line) REFERENCES serving.orders(id, line)", true, []string{"order_id", "line"}, []string{"id", "line"}},
		{`FOREIGN KEY ("Key") REFERENCES "Odd(Schema"."T"("Id")`, true, []string{"Key"}, []string{"Id"}},
		{"FOREIGN KEY (a) REFERENCES t (b) MATCH FULL", true, []string{"a"}, []string{"b"}},
	} {
		columns, referenced, err := constraintsKeyColumns(test.definition, test.foreign)
		require.NoError(t, err, test.definition)
		assert.Equal(t, test.columns, columns, test.definition)
		assert.Equal(t, test.referenced, referenced, test.definition)
	}
	for _, test := range []struct {
		definition string
		foreign    bool
	}{
		{"PRIMARY KEY", false},
		{"PRIMARY KEY ()", false},
		{"PRIMARY KEY (id", false},
		{`UNIQUE ("open)`, false},
		{"UNIQUE (a b)", false},
		{"UNIQUE (a,)", false},
		{"FOREIGN KEY (a) REFERENCES t", true},
	} {
		_, _, err := constraintsKeyColumns(test.definition, test.foreign)
		assert.Error(t, err, test.definition)
	}
}
