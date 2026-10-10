package provider

import (
	"fmt"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// constraintsTypes maps the constraint_type values to pg_constraint.contype. Redshift declares only these three
// table constraints; it does not enforce them, but the planner relies on them.
var constraintsTypes = map[string]string{"PRIMARY KEY": "p", "UNIQUE": "u", "FOREIGN KEY": "f"}

// constraintsNames lists the accepted constraint_type values in documentation order.
var constraintsNames = []string{"PRIMARY KEY", "UNIQUE", "FOREIGN KEY"}

// constraintsSource joins the constrained table and, for foreign keys, the referenced table. Only columns that the
// AWS catalog documentation lists as accessible are read; key columns come from pg_get_constraintdef, because the
// conkey arrays are an unsupported type for both transports.
const constraintsSource sqlclient.Keyword = "pg_constraint k JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_class f ON f.oid = k.confrelid LEFT JOIN pg_namespace fn ON fn.oid = f.relnamespace"

// constraintsTypeColumn renders contype as its constraint_type name on the server. contype has PostgreSQL's
// one-byte "char" type, which the direct transport decodes as a rune and would hand back as its decimal code.
const constraintsTypeColumn = "CASE k.contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'f' THEN 'FOREIGN KEY' END AS constraint_type"

// readConstraintsQuery lists the key constraints of the current database. pg_constraint only covers the database
// the statement runs in, so the caller connects to the listed database. An unknown type is an error, because
// dropping the filter would widen the listing instead of narrowing it.
func readConstraintsQuery(schemaName, table, constraintType string) (sqlclient.Query, error) {
	code, known := constraintsTypes[constraintType]
	if constraintType != "" && !known {
		return sqlclient.Query{}, fmt.Errorf("unsupported constraint_type %q", constraintType)
	}
	return sqlclient.Select("n.nspname AS schema_name", "c.relname AS table_name", "k.conname AS constraint_name", constraintsTypeColumn, "pg_get_constraintdef(k.oid) AS definition", "fn.nspname AS referenced_schema", "f.relname AS referenced_table").
		From(constraintsSource).
		Where("k.contype IN ('p', 'u', 'f')").
		OptEq("n.nspname", "schema", schemaName).
		OptEq("c.relname", "table", table).
		OptEq("k.contype", "contype", code).
		OrderBy("n.nspname", "c.relname", "k.conname"), nil
}

// constraintsKeyColumns extracts the constrained columns and, for a foreign key, the referenced columns from a
// pg_get_constraintdef definition such as `FOREIGN KEY (a, "B") REFERENCES s.t(x, y)`, keeping key order, which
// pairs the columns of a composite foreign key.
func constraintsKeyColumns(definition string, foreign bool) ([]string, []string, error) {
	columns, rest, err := constraintsIdentifierList(definition)
	if err != nil {
		return nil, nil, fmt.Errorf("constraint definition %q: %w", definition, err)
	}
	if !foreign {
		return columns, nil, nil
	}
	referenced, _, err := constraintsIdentifierList(rest)
	if err != nil {
		return nil, nil, fmt.Errorf("constraint definition %q: referenced columns: %w", definition, err)
	}
	return columns, referenced, nil
}

// constraintsIdentifierList parses the first parenthesized identifier list outside double quotes and returns it
// with the text after its closing parenthesis. Quoted identifiers lose their quotes and doubled quotes.
func constraintsIdentifierList(text string) ([]string, string, error) {
	start, quoted := -1, false
	for i := 0; i < len(text) && start < 0; i++ {
		switch {
		case text[i] == '"':
			quoted = !quoted
		case text[i] == '(' && !quoted:
			start = i + 1
		}
	}
	if start < 0 {
		return nil, "", fmt.Errorf("missing column list")
	}
	var names []string
	i := start
	for {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		var name strings.Builder
		if i < len(text) && text[i] == '"' {
			i++
			for {
				if i >= len(text) {
					return nil, "", fmt.Errorf("unterminated quoted identifier")
				}
				if text[i] == '"' {
					if i+1 < len(text) && text[i+1] == '"' {
						name.WriteByte('"')
						i += 2
						continue
					}
					i++
					break
				}
				name.WriteByte(text[i])
				i++
			}
		} else {
			for i < len(text) && text[i] != ',' && text[i] != ')' && text[i] != ' ' {
				name.WriteByte(text[i])
				i++
			}
		}
		if name.Len() == 0 {
			return nil, "", fmt.Errorf("empty column name")
		}
		names = append(names, name.String())
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i >= len(text) {
			return nil, "", fmt.Errorf("unterminated column list")
		}
		switch text[i] {
		case ',':
			i++
		case ')':
			return names, text[i+1:], nil
		default:
			return nil, "", fmt.Errorf("unexpected %q in column list", text[i])
		}
	}
}
