package provider

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// maskingPolicyColumn is one input column of a masking policy: the name the expression refers to and its type.
type maskingPolicyColumn struct {
	// Name is the input column name used inside the masking expression.
	Name string
	// Type is the configured or catalog-reported data type.
	Type string
}

// maskingPolicyColumnType is the element type of the input_column blocks.
var maskingPolicyColumnType = types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "type": types.StringType}}

// maskingPolicyColumns returns the known input columns of a list, in order; unknown elements are skipped, so callers
// that need every column check the configuration is fully known first.
func maskingPolicyColumns(list types.List) []maskingPolicyColumn {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var columns []maskingPolicyColumn
	for _, element := range list.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			continue
		}
		columns = append(columns, maskingPolicyColumn{Name: objectString(object, "name"), Type: objectString(object, "type")})
	}
	return columns
}

// maskingPolicyColumnList converts input columns to their Terraform list.
func maskingPolicyColumnList(columns []maskingPolicyColumn) types.List {
	elements := make([]attr.Value, 0, len(columns))
	for _, column := range columns {
		elements = append(elements, types.ObjectValueMust(maskingPolicyColumnType.AttrTypes, map[string]attr.Value{"name": types.StringValue(column.Name), "type": types.StringValue(column.Type)}))
	}
	return types.ListValueMust(maskingPolicyColumnType, elements)
}

// maskingPolicyValidate checks what CREATE MASKING POLICY needs before any SQL runs: at least one uniquely named
// input column with a Redshift type, and an expression that stays inside its USING parentheses.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_MASKING_POLICY.html
func maskingPolicyValidate(data maskingPolicyModel) error {
	if knownString(data.Name) == "" {
		return fmt.Errorf("name must not be empty")
	}
	columns := maskingPolicyColumns(data.InputColumn)
	if len(columns) == 0 {
		return fmt.Errorf("at least one input_column block is required")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if column.Name == "" {
			return fmt.Errorf("input column names must not be empty")
		}
		// Redshift folds identifiers to lower case by default, so names differing only in case collide.
		key := strings.ToLower(column.Name)
		if seen[key] {
			return fmt.Errorf("input column %q is declared twice", column.Name)
		}
		seen[key] = true
		if _, err := maskingPolicyInputType(column.Type); err != nil {
			return fmt.Errorf("input column %q: %w", column.Name, err)
		}
	}
	if _, err := sqlclient.CheckUserSQL(data.Expression.ValueString()); err != nil {
		return fmt.Errorf("expression: %w", err)
	}
	return nil
}

// maskingPolicyRoutineTypes are pseudo-types that ColumnType accepts for routine signatures. No column has them, and
// the input types must match the masked columns' types, so a policy declaring one could never be attached.
var maskingPolicyRoutineTypes = []sqlclient.Keyword{"ANYELEMENT", "REFCURSOR"}

// maskingPolicyInputType validates an input column type and returns the uppercase form of the spelling the catalog
// reports for it.
func maskingPolicyInputType(value string) (sqlclient.Keyword, error) {
	columnType, err := sqlclient.ColumnType(value)
	if err == nil && slices.Contains(maskingPolicyRoutineTypes, columnType) {
		return "", fmt.Errorf("data type %q is a routine pseudo-type that no column can have", value)
	}
	return columnType, err
}

// maskingPolicyInputs renders the WITH (name type, ...) items; ColumnType applies the default length a column
// declared without one gets, which is how the catalog reports the input afterwards.
func maskingPolicyInputs(columns []maskingPolicyColumn) ([]sqlclient.Statement, error) {
	items := make([]sqlclient.Statement, 0, len(columns))
	for _, column := range columns {
		columnType, err := maskingPolicyInputType(column.Type)
		if err != nil {
			return nil, fmt.Errorf("input column %q: %w", column.Name, err)
		}
		items = append(items, sqlclient.Ident(column.Name).Kw(columnType))
	}
	return items, nil
}

// maskingPolicyUsing renders USING (expression) from configured SQL.
func maskingPolicyUsing(expression types.String) (sqlclient.Statement, error) {
	text, err := sqlclient.CheckUserSQL(expression.ValueString())
	if err != nil {
		return sqlclient.Statement{}, fmt.Errorf("expression: %w", err)
	}
	return sqlclient.Kw("USING").Paren(sqlclient.Fragment().Verbatim(text)), nil
}

// createMaskingPolicyStatement renders CREATE MASKING POLICY in the connected database, so the policy name stays
// unqualified. https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_MASKING_POLICY.html
func createMaskingPolicyStatement(data maskingPolicyModel) (string, error) {
	if err := maskingPolicyValidate(data); err != nil {
		return "", err
	}
	inputs, err := maskingPolicyInputs(maskingPolicyColumns(data.InputColumn))
	if err != nil {
		return "", err
	}
	using, err := maskingPolicyUsing(data.Expression)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("CREATE MASKING POLICY").Ident(data.Name.ValueString()).Kw("WITH").Paren(inputs...).Append(using)
	return statement.String(), statement.Err()
}

// maskingPolicyAlterSteps lists the in-place changes. ALTER MASKING POLICY only replaces the expression; the input
// columns and their types must stay the same, so they replace the policy instead.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_MASKING_POLICY.html
var maskingPolicyAlterSteps = []alterStep[maskingPolicyModel]{{
	attribute: "expression",
	value:     func(m maskingPolicyModel) attr.Value { return m.Expression },
	render: func(_, plan maskingPolicyModel) []string {
		// alterMaskingPolicyStatements validated the expression before the steps run.
		using, _ := maskingPolicyUsing(plan.Expression)
		return []string{sqlclient.Stmt("ALTER MASKING POLICY").Ident(plan.Name.ValueString()).Append(using).String()}
	},
}}

// alterMaskingPolicyStatements renders the in-place changes from prev to plan.
func alterMaskingPolicyStatements(prev, plan maskingPolicyModel) ([]string, error) {
	if err := maskingPolicyValidate(plan); err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, maskingPolicyAlterSteps), nil
}

// dropMaskingPolicyStatement renders DROP MASKING POLICY. The command has no CASCADE; Redshift refuses to drop a
// policy that is still attached. https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_MASKING_POLICY.html
func dropMaskingPolicyStatement(data maskingPolicyModel) string {
	return sqlclient.Stmt("DROP MASKING POLICY").Ident(data.Name.ValueString()).String()
}

// maskingPolicyCatalogColumns lists the catalog columns every masking policy read selects.
var maskingPolicyCatalogColumns = []sqlclient.Keyword{"policy_database", "policy_name", "input_columns", "policy_expression"}

// readMaskingPolicyQuery reads one policy from SVV_MASKING_POLICY, which lists the policies of every database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_MASKING_POLICY.html
func readMaskingPolicyQuery(data maskingPolicyModel) sqlclient.Query {
	return sqlclient.Select(maskingPolicyCatalogColumns...).From("svv_masking_policy").
		Where("policy_database = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("policy_name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// listMaskingPoliciesQuery lists policies, optionally of one database, in a stable order.
func listMaskingPoliciesQuery(database string) sqlclient.Query {
	return sqlclient.Select(maskingPolicyCatalogColumns...).From("svv_masking_policy").
		OptEq("policy_database", "database", database).OrderBy("policy_database", "policy_name")
}

// maskingPolicyParseColumns parses the input_columns catalog text, a JSON list of {"colname", "type"} objects as
// SVV_RLS_POLICY and SHOW MASKING POLICIES document it, and reports the types in their canonical uppercase spelling.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_POLICIES.html
func maskingPolicyParseColumns(text string) ([]maskingPolicyColumn, error) {
	var parsed []struct {
		Name string `json:"colname"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("unexpected masking policy input_columns %q: %w", text, err)
	}
	columns := make([]maskingPolicyColumn, 0, len(parsed))
	for _, column := range parsed {
		columns = append(columns, maskingPolicyColumn{Name: column.Name, Type: sqlclient.CatalogType(column.Type)})
	}
	return columns, nil
}

// maskingPolicyExpressionText extracts the masking expression from the policy_expression catalog text. SHOW
// MASKING POLICIES documents it as a JSON list of {"expr", "type"} objects, one per output; text in any other
// shape is the expression itself.
func maskingPolicyExpressionText(text string) string {
	var parsed []struct {
		Expression *string `json:"expr"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil || len(parsed) == 0 {
		return text
	}
	expressions := make([]string, 0, len(parsed))
	for _, output := range parsed {
		if output.Expression == nil {
			return text
		}
		expressions = append(expressions, *output.Expression)
	}
	return strings.Join(expressions, ", ")
}

// maskingPolicyCanonicalType returns the type the catalog would report for a column type, so equivalent spellings
// such as TEXT and CHARACTER VARYING(256) compare equal; an unparsable type compares by its uppercased text.
func maskingPolicyCanonicalType(columnType string) string {
	if canonical, err := sqlclient.ColumnType(columnType); err == nil {
		return string(canonical)
	}
	return strings.ToUpper(strings.Join(strings.Fields(columnType), " "))
}

// maskingPolicyColumnsMatch reports whether configured columns describe the catalog columns. Names compare without
// case because Redshift folds identifiers by default.
func maskingPolicyColumnsMatch(configured, catalog []maskingPolicyColumn) bool {
	if len(configured) != len(catalog) {
		return false
	}
	for i := range configured {
		if !strings.EqualFold(configured[i].Name, catalog[i].Name) || maskingPolicyCanonicalType(configured[i].Type) != maskingPolicyCanonicalType(catalog[i].Type) {
			return false
		}
	}
	return true
}
