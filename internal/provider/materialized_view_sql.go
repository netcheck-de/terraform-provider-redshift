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

// materializedViewDistributionModel is the distribution block's style and key.
type materializedViewDistributionModel struct {
	// Style is EVEN, ALL, or KEY; null leaves it to the key or the server default.
	Style types.String
	// Key is the DISTKEY column.
	Key types.String
}

// materializedViewDistribution returns the distribution block's attributes: null when the block is absent and
// unknown while the whole block is, so callers handle the block like two optional attributes.
func materializedViewDistribution(data materializedViewModel) materializedViewDistributionModel {
	switch {
	case data.Distribution.IsUnknown():
		return materializedViewDistributionModel{Style: types.StringUnknown(), Key: types.StringUnknown()}
	case data.Distribution.IsNull():
		return materializedViewDistributionModel{Style: types.StringNull(), Key: types.StringNull()}
	}
	attributes := data.Distribution.Attributes()
	style, _ := attributes["style"].(types.String)
	key, _ := attributes["key"].(types.String)
	return materializedViewDistributionModel{Style: style, Key: key}
}

// materializedViewSortKeyColumns returns the sort_key block's columns: null when the block is absent and unknown
// while the whole block is.
func materializedViewSortKeyColumns(data materializedViewModel) types.List {
	switch {
	case data.SortKey.IsUnknown():
		return types.ListUnknown(types.StringType)
	case data.SortKey.IsNull():
		return types.ListNull(types.StringType)
	}
	columns, _ := data.SortKey.Attributes()["columns"].(types.List)
	return columns
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
			return nil, fmt.Errorf("sort_key.columns must be nonempty")
		}
		columns = append(columns, column.ValueString())
	}
	return columns, nil
}

// materializedViewAttributes renders table_attributes. DISTKEY implies DISTSTYLE KEY, so a distribution key with
// another style, or KEY without a key, is rejected before Redshift would. So are empty blocks, which would
// otherwise mean the same as absent ones; the schema cannot require their attributes, because the framework
// enforces Required inside a single block even when the block is absent.
func materializedViewAttributes(data materializedViewModel) (sqlclient.Statement, error) {
	distribution := materializedViewDistribution(data)
	style, err := optOneOf(distribution.Style, materializedViewDistStyles...)
	if err != nil {
		return sqlclient.Statement{}, fmt.Errorf("distribution.style: %w", err)
	}
	distKey := knownString(distribution.Key)
	switch {
	case distKey != "" && style != "" && style != "KEY":
		return sqlclient.Statement{}, fmt.Errorf("distribution.key requires distribution.style KEY or no style, not %s", style)
	case style == "KEY" && distKey == "" && !distribution.Key.IsUnknown():
		return sqlclient.Statement{}, fmt.Errorf("distribution.style KEY requires distribution.key")
	case !data.Distribution.IsNull() && distribution.Style.IsNull() && distribution.Key.IsNull():
		return sqlclient.Statement{}, fmt.Errorf("the distribution block requires a style or a key")
	}
	sortKeyColumns := materializedViewSortKeyColumns(data)
	if !data.SortKey.IsNull() && sortKeyColumns.IsNull() {
		return sqlclient.Statement{}, fmt.Errorf("the sort_key block requires columns")
	}
	sortKey, err := materializedViewSortKey(sortKeyColumns)
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

// materializedViewDistributionValue is the distribution an update compares: the key column, the style, or null
// when neither is configured. DISTKEY implies KEY, so adding or removing an explicit KEY next to a key runs nothing.
func materializedViewDistributionValue(data materializedViewModel) attr.Value {
	distribution := materializedViewDistribution(data)
	if distribution.Style.IsUnknown() || distribution.Key.IsUnknown() {
		return types.StringUnknown()
	}
	style, _ := optOneOf(distribution.Style, materializedViewDistStyles...) // alterMaterializedViewStatements validated it first.
	switch distKey := knownString(distribution.Key); {
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
		// Redshift alters the style and key in one clause, so the block has a single step.
		attribute: "distribution",
		value:     materializedViewDistributionValue,
		render: func(_, plan materializedViewModel) []string {
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			distribution := materializedViewDistribution(plan)
			style, _ := optOneOf(distribution.Style, materializedViewDistStyles...)
			statement := sqlclient.Stmt("ALTER MATERIALIZED VIEW").Append(relation).Kw("ALTER DISTSTYLE")
			switch distKey := knownString(distribution.Key); {
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
		attribute: "sort_key",
		value:     func(data materializedViewModel) attr.Value { return materializedViewSortKeyColumns(data) },
		render: func(_, plan materializedViewModel) []string {
			relation, _ := materializedViewRelation(plan) // alterMaterializedViewStatements validated it first.
			columns, _ := materializedViewSortKey(materializedViewSortKeyColumns(plan))
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
