package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// materializedViewDistStyles are the DISTSTYLE values CREATE MATERIALIZED VIEW accepts; AUTO exists only in
// ALTER MATERIALIZED VIEW.
// https://docs.aws.amazon.com/redshift/latest/dg/materialized-view-create-sql-command.html
var materializedViewDistStyles = []sqlclient.Keyword{"EVEN", "ALL", "KEY"}

// materializedViewRelation renders the schema-qualified materialized view.
func materializedViewRelation(data materializedViewModel) (sqlclient.Statement, error) {
	return viewRelation(data.Schema.ValueString(), data.Name.ValueString())
}

// materializedViewSortKey returns the sort key columns in configured order, which defines the compound key.
func materializedViewSortKey(value types.List) ([]string, error) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	var columns []string
	for _, element := range value.Elements() {
		column, ok := element.(types.String)
		if !ok || column.IsUnknown() {
			continue
		}
		if strings.TrimSpace(column.ValueString()) == "" {
			return nil, fmt.Errorf("sortkey columns must be nonempty")
		}
		columns = append(columns, column.ValueString())
	}
	return columns, nil
}

// materializedViewAttributes renders table_attributes. DISTKEY implies DISTSTYLE KEY, so a distribution key with
// another style, or KEY without a key, is rejected before Redshift would.
func materializedViewAttributes(data materializedViewModel) (sqlclient.Statement, error) {
	style, err := optOneOf(data.DistStyle, materializedViewDistStyles...)
	if err != nil {
		return sqlclient.Statement{}, fmt.Errorf("diststyle: %w", err)
	}
	distKey := knownString(data.DistKey)
	switch {
	case distKey != "" && style != "" && style != "KEY":
		return sqlclient.Statement{}, fmt.Errorf("distkey requires diststyle KEY or no diststyle, not %s", style)
	case style == "KEY" && distKey == "" && !data.DistKey.IsUnknown():
		return sqlclient.Statement{}, fmt.Errorf("diststyle KEY requires distkey")
	}
	sortKey, err := materializedViewSortKey(data.SortKey)
	if err != nil {
		return sqlclient.Statement{}, err
	}
	return sqlclient.Fragment().
		When(style != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("DISTSTYLE", style) }).
		When(distKey != "", func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("DISTKEY").Paren(sqlclient.Ident(distKey))
		}).
		When(len(sortKey) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("SORTKEY").Paren(sqlclient.Fragment().Idents(sortKey...))
		}), nil
}

// createMaterializedViewStatements renders CREATE MATERIALIZED VIEW in the documented clause order (BACKUP,
// table_attributes, AUTO REFRESH, AS query), followed by the ownership transfer when an owner is configured.
// AUTO REFRESH is always spelled out so creation never depends on the server default.
func createMaterializedViewStatements(data materializedViewModel) ([]string, error) {
	relation, err := materializedViewRelation(data)
	if err != nil {
		return nil, err
	}
	attributes, err := materializedViewAttributes(data)
	if err != nil {
		return nil, err
	}
	query, err := viewQuery(data.Query.ValueString())
	if err != nil {
		return nil, err
	}
	create := sqlclient.Stmt("CREATE MATERIALIZED VIEW").Append(relation).
		OptToggle(knownBool(data.Backup), "BACKUP YES", "BACKUP NO").
		Append(attributes).
		Toggle(data.AutoRefresh.ValueBool(), "AUTO REFRESH YES", "AUTO REFRESH NO").
		Kw("AS").Verbatim(query)
	statements := []string{create.String()}
	if owner := knownString(data.Owner); owner != "" {
		statements = append(statements, viewOwnerStatement(relation, owner))
	}
	return statements, create.Err()
}

// materializedViewDistribution is the distribution an update compares: the key column, the style, or null when
// neither is configured. DISTKEY implies KEY, so adding or removing an explicit KEY next to a key runs nothing.
func materializedViewDistribution(data materializedViewModel) attr.Value {
	if data.DistStyle.IsUnknown() || data.DistKey.IsUnknown() {
		return types.StringUnknown()
	}
	style, _ := optOneOf(data.DistStyle, materializedViewDistStyles...) // alterMaterializedViewStatements validated it first.
	switch distKey := knownString(data.DistKey); {
	case distKey != "":
		return types.StringValue("KEY\x00" + distKey)
	case style != "":
		return types.StringValue(string(style))
	}
	return types.StringNull()
}

// materializedViewAlterSteps update everything except the query and BACKUP in place, the two inputs ALTER
// MATERIALIZED VIEW has no clause for. The ownership transfer runs last, because a provider identity that is not
// a superuser loses the right to alter the view once it no longer owns it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_MATERIALIZED_VIEW.html
var materializedViewAlterSteps = []alterStep[materializedViewModel]{
	{
		// distkey has no step of its own: Redshift alters the style and key in one clause.
		attribute: "diststyle",
		value:     materializedViewDistribution,
		render: func(_, plan materializedViewModel) []string {
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			style, _ := optOneOf(plan.DistStyle, materializedViewDistStyles...)
			statement := sqlclient.Stmt("ALTER MATERIALIZED VIEW").Append(relation).Kw("ALTER DISTSTYLE")
			switch distKey := knownString(plan.DistKey); {
			case distKey != "":
				statement = statement.Kw("KEY DISTKEY").Ident(distKey)
			case style == "ALL":
				statement = statement.Kw("ALL")
			default:
				// A removed distribution returns to EVEN, the documented CREATE default.
				statement = statement.Kw("EVEN")
			}
			return []string{statement.String()}
		},
	},
	{
		attribute: "sortkey",
		value:     func(data materializedViewModel) attr.Value { return data.SortKey },
		render: func(_, plan materializedViewModel) []string {
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			columns, _ := materializedViewSortKey(plan.SortKey)
			statement := sqlclient.Stmt("ALTER MATERIALIZED VIEW").Append(relation)
			if len(columns) == 0 {
				return []string{statement.Kw("ALTER SORTKEY NONE").String()}
			}
			return []string{statement.Kw("ALTER COMPOUND SORTKEY").Paren(sqlclient.Fragment().Idents(columns...)).String()}
		},
	},
	{
		attribute: "auto_refresh",
		value:     func(data materializedViewModel) attr.Value { return data.AutoRefresh },
		render: func(_, plan materializedViewModel) []string {
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			return []string{sqlclient.Stmt("ALTER MATERIALIZED VIEW").Append(relation).
				Toggle(plan.AutoRefresh.ValueBool(), "AUTO REFRESH YES", "AUTO REFRESH NO").String()}
		},
	},
	{
		attribute: "owner",
		value:     func(data materializedViewModel) attr.Value { return data.Owner },
		render: func(_, plan materializedViewModel) []string {
			owner := knownString(plan.Owner)
			if owner == "" {
				return nil
			}
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			return []string{viewOwnerStatement(relation, owner)}
		},
	},
}

// alterMaterializedViewStatements renders the in-place changes from prev to plan after the checks CREATE applies
// to the name and storage options.
func alterMaterializedViewStatements(prev, plan materializedViewModel) ([]string, error) {
	if _, err := materializedViewRelation(plan); err != nil {
		return nil, err
	}
	if _, err := materializedViewAttributes(plan); err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, materializedViewAlterSteps), nil
}

// dropMaterializedViewStatement renders DROP MATERIALIZED VIEW without CASCADE; RESTRICT is the default, so
// dependent objects keep the materialized view and the error names them.
// https://docs.aws.amazon.com/redshift/latest/dg/materialized-view-drop-sql-command.html
func dropMaterializedViewStatement(data materializedViewModel) (string, error) {
	relation, err := materializedViewRelation(data)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("DROP MATERIALIZED VIEW").Append(relation).String(), nil
}

// readMaterializedViewQuery reads the owner and definition from pg_views, which lists materialized views with
// a CREATE MATERIALIZED VIEW definition.
func readMaterializedViewQuery(data materializedViewModel) sqlclient.Query {
	return viewCatalogQuery(data.Schema.ValueString(), data.Name.ValueString())
}

// readMaterializedViewRefreshQuery reads the automatic refresh setting from SVV_MV_INFO, which also exists on
// Serverless, unlike STV_MV_INFO. Its name columns are CHAR(128), so they are trimmed before comparison.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_MV_INFO.html
func readMaterializedViewRefreshQuery(data materializedViewModel) sqlclient.Query {
	return sqlclient.Select("autorefresh").From("svv_mv_info").
		Where("RTRIM(database_name) = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("RTRIM(schema_name) = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("RTRIM(name) = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// materializedViewFlag parses an SVV_MV_INFO CHAR(1) flag; the reference documents t and f while its sample
// output prints 1 and 0, so both spellings are accepted.
func materializedViewFlag(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "t", "true", "1":
		return true, nil
	case "f", "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("unexpected SVV_MV_INFO flag %q", value)
}
