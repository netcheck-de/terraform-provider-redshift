package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// viewRelation renders the schema-qualified relation. Empty parts are rejected because Qualified omits an empty
// schema, which would create or drop the relation through the search path instead.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_VIEW.html
func viewRelation(schema, name string) (sqlclient.Statement, error) {
	if schema == "" || name == "" {
		return sqlclient.Statement{}, fmt.Errorf("a view requires a nonempty schema and name")
	}
	return sqlclient.Fragment().Qualified(schema, name), nil
}

// viewQuery checks the configured SELECT. Surrounding whitespace is dropped so a heredoc never leaves a trailing
// newline in the statement; the configured text itself stays in state.
func viewQuery(query string) (sqlclient.UserSQL, error) {
	checked, err := sqlclient.CheckUserSQL(strings.TrimSpace(query))
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	return checked, nil
}

// viewOwnerStatement renders ALTER TABLE … OWNER TO, which the ALTER TABLE reference documents for views; ALTER
// MATERIALIZED VIEW has no OWNER TO clause, so materialized views use it too.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE.html
func viewOwnerStatement(relation sqlclient.Statement, owner string) string {
	return sqlclient.Stmt("ALTER TABLE").Append(relation).KwIdent("OWNER TO", owner).String()
}

// viewDefinitionStatement renders CREATE [OR REPLACE] VIEW. A late-binding view appends WITH NO SCHEMA BINDING
// after the query, as the CREATE VIEW synopsis places it.
func viewDefinitionStatement(verb sqlclient.Keyword, data viewModel) (string, error) {
	relation, err := viewRelation(data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		return "", err
	}
	query, err := viewQuery(data.Query.ValueString())
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt(verb).Append(relation).Kw("AS").Verbatim(query).
		If(data.LateBinding.ValueBool(), "WITH NO SCHEMA BINDING")
	return statement.String(), statement.Err()
}

// createViewStatements renders CREATE VIEW, followed by the ownership transfer when an owner is configured. Plain
// CREATE VIEW fails on an existing relation, so creation never silently adopts a view Terraform does not manage.
func createViewStatements(data viewModel) ([]string, error) {
	create, err := viewDefinitionStatement("CREATE VIEW", data)
	if err != nil {
		return nil, err
	}
	statements := []string{create}
	if owner := knownString(data.Owner); owner != "" {
		relation, _ := viewRelation(data.Schema.ValueString(), data.Name.ValueString())
		statements = append(statements, viewOwnerStatement(relation, owner))
	}
	return statements, nil
}

// replaceViewStatement renders CREATE OR REPLACE VIEW, which keeps the view's owner and grants. Redshift rejects
// a query whose columns differ in name or type; that error is reported instead of dropping the view, because a
// drop would also remove dependent views and grants.
func replaceViewStatement(data viewModel) (string, error) {
	return viewDefinitionStatement("CREATE OR REPLACE VIEW", data)
}

// viewAlterSteps update a view in place. The query and binding mode share one CREATE OR REPLACE VIEW, so the
// binding step only emits it when the query step does not.
var viewAlterSteps = []alterStep[viewModel]{
	{
		attribute: "query",
		value:     func(data viewModel) attr.Value { return data.Query },
		render: func(_, plan viewModel) []string {
			statement, _ := replaceViewStatement(plan) // alterViewStatements validated plan first.
			return []string{statement}
		},
	},
	{
		attribute: "late_binding",
		value:     func(data viewModel) attr.Value { return data.LateBinding },
		render: func(prev, plan viewModel) []string {
			if !prev.Query.Equal(plan.Query) {
				return nil
			}
			statement, _ := replaceViewStatement(plan) // alterViewStatements validated plan first.
			return []string{statement}
		},
	},
	{
		attribute: "owner",
		value:     func(data viewModel) attr.Value { return data.Owner },
		render: func(_, plan viewModel) []string {
			owner := knownString(plan.Owner)
			if owner == "" {
				return nil
			}
			relation, _ := viewRelation(plan.Schema.ValueString(), plan.Name.ValueString())
			return []string{viewOwnerStatement(relation, owner)}
		},
	},
}

// alterViewStatements renders the in-place changes from prev to plan: the definition first, then the owner, so
// a refused definition leaves ownership untouched.
func alterViewStatements(prev, plan viewModel) ([]string, error) {
	if _, err := replaceViewStatement(plan); err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, viewAlterSteps), nil
}

// dropViewStatement renders DROP VIEW without CASCADE, so a view that other views depend on is kept and the
// error names the dependents.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_VIEW.html
func dropViewStatement(data viewModel) (string, error) {
	relation, err := viewRelation(data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("DROP VIEW").Append(relation).String(), nil
}

// viewCatalogQuery reads one relation from pg_views, the PostgreSQL catalog view that Redshift documents as
// accessible. Its definition column is pg_get_viewdef, which SHOW VIEW also prints: the SELECT for an ordinary
// view, and the full CREATE statement for late-binding and materialized views.
// https://docs.aws.amazon.com/redshift/latest/dg/c_join_PG.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_VIEW.html
func viewCatalogQuery(schema, name string) sqlclient.Query {
	return sqlclient.Select("schemaname", "viewname", "viewowner", "definition").From("pg_views").
		Where("schemaname = :schema", sqlclient.Bind("schema", schema)).
		Where("viewname = :name", sqlclient.Bind("name", name))
}

// readViewQuery reads the view's owner and definition.
func readViewQuery(data viewModel) sqlclient.Query {
	return viewCatalogQuery(data.Schema.ValueString(), data.Name.ValueString())
}

// viewDefinitionKind classifies pg_get_viewdef text. Redshift prints the SELECT of an ordinary view, but the
// complete CREATE statement of a late-binding view (ending in WITH NO SCHEMA BINDING) and of a materialized view
// (starting with CREATE MATERIALIZED VIEW), as the CREATE VIEW and CREATE MATERIALIZED VIEW examples show.
func viewDefinitionKind(definition string) (lateBinding, materialized bool) {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(definition), ";"))))
	text := strings.Join(words, " ")
	return strings.HasSuffix(text, " with no schema binding"), strings.HasPrefix(text, "create materialized view ")
}
