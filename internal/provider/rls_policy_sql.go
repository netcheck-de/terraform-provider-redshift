package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// rlsPolicyColumnType is the element type of the policy's WITH column list.
var rlsPolicyColumnType = types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "type": types.StringType}}

// rlsPolicyColumn is one WITH column with its data type canonicalized as the catalog reports it.
type rlsPolicyColumn struct {
	// name is the column of the attached relation the predicate reads.
	name string
	// dataType is the canonical Redshift type, comparable with svv_rls_policy.polatts.
	dataType sqlclient.Keyword
}

// rlsPolicyColumns validates the configured WITH columns. A null or unknown list yields none, so the WITH
// clause is omitted.
func rlsPolicyColumns(value types.List) ([]rlsPolicyColumn, error) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	columns := make([]rlsPolicyColumn, 0, len(value.Elements()))
	for index, element := range value.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			return nil, fmt.Errorf("columns[%d] must be a known object", index)
		}
		name, dataType := objectString(object, "name"), objectString(object, "type")
		if name == "" {
			return nil, fmt.Errorf("columns[%d] needs a nonempty name", index)
		}
		canonical, err := sqlclient.ColumnType(dataType)
		if err != nil {
			return nil, fmt.Errorf("columns[%d] (%s): %w", index, name, err)
		}
		columns = append(columns, rlsPolicyColumn{name: name, dataType: canonical})
	}
	return columns, nil
}

// rlsPolicyCatalogColumn is one element of svv_rls_policy.polatts.
type rlsPolicyCatalogColumn struct {
	// Name is the WITH column name.
	Name string `json:"colname"`
	// Type is the format_type() spelling of the WITH column type.
	Type string `json:"type"`
}

// parseRlsPolicyColumns decodes polatts, which is empty or an empty JSON array for a policy without WITH.
func parseRlsPolicyColumns(polatts string) ([]rlsPolicyCatalogColumn, error) {
	if strings.TrimSpace(polatts) == "" {
		return nil, nil
	}
	var columns []rlsPolicyCatalogColumn
	if err := json.Unmarshal([]byte(polatts), &columns); err != nil {
		return nil, fmt.Errorf("decode RLS policy attributes %q: %w", polatts, err)
	}
	return columns, nil
}

// rlsPolicyColumnsList converts catalog columns to the Terraform list, null when the policy has none.
func rlsPolicyColumnsList(columns []rlsPolicyCatalogColumn) types.List {
	if len(columns) == 0 {
		return types.ListNull(rlsPolicyColumnType)
	}
	elements := make([]attr.Value, len(columns))
	for i, column := range columns {
		elements[i] = types.ObjectValueMust(rlsPolicyColumnType.AttrTypes, map[string]attr.Value{"name": types.StringValue(column.Name), "type": types.StringValue(column.Type)})
	}
	return types.ListValueMust(rlsPolicyColumnType, elements)
}

// rlsPolicyCatalogColumnsOf reads a columns list back as name/type pairs, so prior state compares like catalog rows.
func rlsPolicyCatalogColumnsOf(value types.List) []rlsPolicyCatalogColumn {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	columns := make([]rlsPolicyCatalogColumn, 0, len(value.Elements()))
	for _, element := range value.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			continue
		}
		columns = append(columns, rlsPolicyCatalogColumn{Name: objectString(object, "name"), Type: objectString(object, "type")})
	}
	return columns
}

// rlsPolicyColumnsMatch reports whether configured columns describe the catalog columns. Names compare without
// case because Redshift folds identifiers by default, and types compare in canonical form, so varchar(64) in
// configuration matches character varying(64) in the catalog.
func rlsPolicyColumnsMatch(configured []rlsPolicyColumn, catalog []rlsPolicyCatalogColumn) bool {
	if len(configured) != len(catalog) {
		return false
	}
	for i, column := range configured {
		catalogType, err := sqlclient.ColumnType(catalog[i].Type)
		if !strings.EqualFold(column.name, catalog[i].Name) || err != nil || catalogType != column.dataType {
			return false
		}
	}
	return true
}

// rlsPolicyPredicate checks the USING expression, which is embedded verbatim.
func rlsPolicyPredicate(data rlsPolicyModel) (sqlclient.UserSQL, error) {
	predicate, err := sqlclient.CheckUserSQL(knownString(data.Predicate))
	if err != nil {
		return "", fmt.Errorf("predicate: %w", err)
	}
	return predicate, nil
}

// rlsPolicyUsing renders USING ( predicate ), shared by CREATE and ALTER.
func rlsPolicyUsing(predicate sqlclient.UserSQL) sqlclient.Statement {
	return sqlclient.Kw("USING").Paren(sqlclient.Fragment().Verbatim(predicate))
}

// createRlsPolicyStatement renders CREATE RLS POLICY. The alias belongs to the WITH clause in the grammar, so an
// alias without columns is rejected rather than silently dropped.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_RLS_POLICY.html
func createRlsPolicyStatement(data rlsPolicyModel) (string, error) {
	if data.Name.ValueString() == "" {
		return "", fmt.Errorf("RLS policy requires a nonempty name")
	}
	columns, err := rlsPolicyColumns(data.Columns)
	if err != nil {
		return "", err
	}
	alias := knownString(data.Alias)
	if alias != "" && len(columns) == 0 {
		return "", fmt.Errorf("alias requires at least one entry in columns, because it is part of the WITH clause")
	}
	predicate, err := rlsPolicyPredicate(data)
	if err != nil {
		return "", err
	}
	items := make([]sqlclient.Statement, len(columns))
	for i, column := range columns {
		items[i] = sqlclient.Ident(column.name).Kw(column.dataType)
	}
	statement := sqlclient.Stmt("CREATE RLS POLICY").Ident(data.Name.ValueString()).
		When(len(columns) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("WITH").Paren(items...).OptIdent("AS", alias)
		}).
		Append(rlsPolicyUsing(predicate))
	return statement.String(), statement.Err()
}

// rlsPolicyAlterSteps lists the in-place changes; ALTER RLS POLICY only accepts a new USING clause.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_RLS_POLICY.html
var rlsPolicyAlterSteps = []alterStep[rlsPolicyModel]{
	{
		attribute: "predicate",
		value:     func(data rlsPolicyModel) attr.Value { return data.Predicate },
		render: func(_, plan rlsPolicyModel) []string {
			// alterRlsPolicyStatements checked the predicate before running the steps.
			predicate, _ := rlsPolicyPredicate(plan)
			return []string{sqlclient.Stmt("ALTER RLS POLICY").Ident(plan.Name.ValueString()).Append(rlsPolicyUsing(predicate)).String()}
		},
	},
}

// alterRlsPolicyStatements renders the statements that move the policy from prev to plan.
func alterRlsPolicyStatements(prev, plan rlsPolicyModel) ([]string, error) {
	if !plan.Predicate.IsUnknown() {
		if _, err := rlsPolicyPredicate(plan); err != nil {
			return nil, err
		}
	}
	return alterStatements(prev, plan, rlsPolicyAlterSteps), nil
}

// dropRlsPolicyStatement renders DROP RLS POLICY without CASCADE: RESTRICT is the default, so a policy that is
// still attached is kept and the attachments must be removed first.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_RLS_POLICY.html
func dropRlsPolicyStatement(data rlsPolicyModel) string {
	return sqlclient.Stmt("DROP RLS POLICY").Ident(data.Name.ValueString()).String()
}

// rlsPolicyColumnsSelect is the svv_rls_policy select list shared by the resource and the listing.
var rlsPolicyColumnsSelect = []sqlclient.Keyword{"poldb", "polname", "polalias", "polatts", "polqual"}

// readRlsPolicyQuery reads one policy of database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RLS_POLICY.html
func readRlsPolicyQuery(data rlsPolicyModel) sqlclient.Query {
	return sqlclient.Select(rlsPolicyColumnsSelect...).From("svv_rls_policy").
		Where("poldb = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("polname = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// listRlsPoliciesQuery reads every policy of database in name order.
func listRlsPoliciesQuery(database string) sqlclient.Query {
	return sqlclient.Select(rlsPolicyColumnsSelect...).From("svv_rls_policy").
		Where("poldb = :database", sqlclient.Bind("database", database)).
		OrderBy("polname")
}
