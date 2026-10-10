package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// tableSecurityConjunctions are the documented CONJUNCTION TYPE values.
var tableSecurityConjunctions = []sqlclient.Keyword{"AND", "OR"}

// tableSecurityAlter starts ALTER TABLE schema.relation ROW LEVEL SECURITY. Empty names are rejected because
// Qualified omits an empty schema, which would resolve the relation through the search path instead.
func tableSecurityAlter(data tableSecurityModel) (sqlclient.Statement, error) {
	if data.Schema.ValueString() == "" || data.Relation.ValueString() == "" {
		return sqlclient.Statement{}, fmt.Errorf("table security requires a nonempty schema and relation")
	}
	return sqlclient.Stmt("ALTER TABLE").Qualified(data.Schema.ValueString(), data.Relation.ValueString()).Kw("ROW LEVEL SECURITY"), nil
}

// tableSecurityStatusStatement renders ROW LEVEL SECURITY ON|OFF with the planned conjunction type, so a
// conjunction change never flips the switch and a switch change keeps the conjunction.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE.html
func tableSecurityStatusStatement(data tableSecurityModel) (string, error) {
	conjunction, err := optOneOf(data.ConjunctionType, tableSecurityConjunctions...)
	if err != nil {
		return "", fmt.Errorf("conjunction_type: %w", err)
	}
	alter, err := tableSecurityAlter(data)
	if err != nil {
		return "", err
	}
	return alter.Toggle(data.RowLevelSecurity.ValueBool(), "ON", "OFF").
		When(conjunction != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("CONJUNCTION TYPE").Kw(conjunction) }).
		String(), nil
}

// tableSecurityDatashareStatement renders ROW LEVEL SECURITY ON|OFF FOR DATASHARES, which changes only whether
// datashare consumers are subject to row-level security, not the relation's own switch.
func tableSecurityDatashareStatement(data tableSecurityModel) (string, error) {
	alter, err := tableSecurityAlter(data)
	if err != nil {
		return "", err
	}
	return alter.Toggle(data.DatashareRowLevelSecurity.ValueBool(), "ON", "OFF").Kw("FOR DATASHARES").String(), nil
}

// createTableSecurityStatements applies the configured switch, conjunction type, and datashare setting.
// Unconfigured optional settings keep what the relation already has.
func createTableSecurityStatements(data tableSecurityModel) ([]string, error) {
	status, err := tableSecurityStatusStatement(data)
	if err != nil {
		return nil, err
	}
	statements := []string{status}
	if knownBool(data.DatashareRowLevelSecurity) != nil {
		datashare, err := tableSecurityDatashareStatement(data)
		if err != nil {
			return nil, err
		}
		statements = append(statements, datashare)
	}
	return statements, nil
}

// tableSecurityMust drops the render error of a statement that alterTableSecurityStatements already validated.
func tableSecurityMust(statement string, _ error) []string {
	return []string{statement}
}

// tableSecurityAlterSteps lists the in-place changes. The switch and the conjunction type share one statement,
// so the conjunction step only runs when the switch is unchanged.
var tableSecurityAlterSteps = []alterStep[tableSecurityModel]{
	{
		attribute: "row_level_security",
		value:     func(data tableSecurityModel) attr.Value { return data.RowLevelSecurity },
		render: func(_, plan tableSecurityModel) []string {
			return tableSecurityMust(tableSecurityStatusStatement(plan))
		},
	},
	{
		attribute: "conjunction_type",
		value:     func(data tableSecurityModel) attr.Value { return data.ConjunctionType },
		render: func(prev, plan tableSecurityModel) []string {
			if !prev.RowLevelSecurity.Equal(plan.RowLevelSecurity) {
				return nil
			}
			return tableSecurityMust(tableSecurityStatusStatement(plan))
		},
	},
	{
		attribute: "datashare_row_level_security",
		value:     func(data tableSecurityModel) attr.Value { return data.DatashareRowLevelSecurity },
		render: func(_, plan tableSecurityModel) []string {
			return tableSecurityMust(tableSecurityDatashareStatement(plan))
		},
	},
}

// alterTableSecurityStatements renders the statements that move the relation from prev to plan.
func alterTableSecurityStatements(prev, plan tableSecurityModel) ([]string, error) {
	if _, err := createTableSecurityStatements(plan); err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, tableSecurityAlterSteps), nil
}

// deleteTableSecurityStatement turns row-level security off, which is what removing the resource means: every
// attached policy stops filtering and all rows become visible to anyone with SELECT. The conjunction type and
// datashare setting are left as they are.
func deleteTableSecurityStatement(data tableSecurityModel) (string, error) {
	alter, err := tableSecurityAlter(data)
	if err != nil {
		return "", err
	}
	return alter.Kw("OFF").String(), nil
}

// readTableSecurityRelationQuery confirms that the table, view, or materialized view still exists; Redshift lists
// views and materialized views with relkind v.
func readTableSecurityRelationQuery(data tableSecurityModel) sqlclient.Query {
	return sqlclient.Select("c.relname").From("pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid").
		Where("n.nspname = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("c.relname = :relation", sqlclient.Bind("relation", data.Relation.ValueString())).
		Where("c.relkind IN ('r', 'v')")
}

// readTableSecurityQuery reads the relation's row-level security settings.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RLS_RELATION.html
func readTableSecurityQuery(data tableSecurityModel) sqlclient.Query {
	return sqlclient.Select("is_rls_on", "is_rls_datashare_on", "rls_conjunction_type").From("svv_rls_relation").
		Where("datname = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("relschema = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("relname = :relation", sqlclient.Bind("relation", data.Relation.ValueString()))
}
