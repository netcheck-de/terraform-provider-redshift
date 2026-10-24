package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// templateRelation renders the schema-qualified template name. Statements run in the template's database, because
// templates cannot be referenced across databases; an empty schema is rejected because Qualified would omit it and
// resolve the name through the search path.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_TEMPLATE.html
func templateRelation(schema, name string) (sqlclient.Statement, error) {
	if schema == "" || name == "" {
		return sqlclient.Statement{}, fmt.Errorf("a template requires a nonempty schema and name")
	}

	return sqlclient.Fragment().Qualified(schema, name), nil
}

// templateValidate checks what CREATE TEMPLATE needs before any SQL runs: a lowercase name, a known format with
// its argument, known parameters with valid values, and at least one of them, as the command requires.
func templateValidate(data templateModel) error {
	if !data.Schema.IsUnknown() && !data.Name.IsUnknown() {
		if _, err := templateRelation(data.Schema.ValueString(), data.Name.ValueString()); err != nil {
			return err
		}
	}

	if name := data.Name.ValueString(); name != strings.ToLower(name) {
		return fmt.Errorf("name %q must be lowercase, because Redshift folds template names to lowercase", name)
	}

	if err := copyValidateKnown(data.Format, data.FormatArgument, data.Parameters, false); err != nil {
		return err
	}

	if !data.Format.IsUnknown() && !data.Parameters.IsUnknown() && knownString(data.Format) == "" && len(data.Parameters.Elements()) == 0 {
		return fmt.Errorf("a template needs a format or at least one parameter")
	}

	return nil
}

// templateDefinitionStatement renders CREATE [OR REPLACE] TEMPLATE ... FOR COPY AS with the format first and the
// parameters in keyword order.
func templateDefinitionStatement(verb sqlclient.Keyword, data templateModel) (string, error) {
	if data.Schema.IsUnknown() || data.Name.IsUnknown() || !copyOptionsKnown(data.Format, data.FormatArgument, data.Parameters) {
		return "", fmt.Errorf("the template definition must be known before apply")
	}

	if err := templateValidate(data); err != nil {
		return "", err
	}

	relation, _ := templateRelation(data.Schema.ValueString(), data.Name.ValueString())

	format, err := copyFormatFragment(knownString(data.Format), knownString(data.FormatArgument))
	if err != nil {
		return "", err
	}

	parameters, err := copyParameterFragments(knownMap(data.Parameters), false)
	if err != nil {
		return "", err
	}

	statement := sqlclient.Stmt(verb).Append(relation).Kw("FOR COPY AS").Append(format).Append(parameters...)

	return statement.String(), statement.Err()
}

// templateOwnerStatement renders ALTER TEMPLATE ... OWNER TO.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TEMPLATE.html
func templateOwnerStatement(data templateModel) string {
	relation, _ := templateRelation(data.Schema.ValueString(), data.Name.ValueString())
	return sqlclient.Stmt("ALTER TEMPLATE").Append(relation).KwIdent("OWNER TO", data.Owner.ValueString()).String()
}

// createTemplateStatements renders CREATE TEMPLATE, followed by the ownership transfer when an owner is configured.
// Plain CREATE fails on an existing template, so creation never adopts one Terraform does not manage.
func createTemplateStatements(data templateModel) ([]string, error) {
	create, err := templateDefinitionStatement("CREATE TEMPLATE", data)
	if err != nil {
		return nil, err
	}

	statements := []string{create}
	if knownString(data.Owner) != "" {
		statements = append(statements, templateOwnerStatement(data))
	}

	return statements, nil
}

// templateReplaced reports whether the data format changes, which runs CREATE OR REPLACE TEMPLATE with the complete
// planned definition instead of ALTER TEMPLATE ... ADD and DROP.
func templateReplaced(prev, plan templateModel) bool {
	return !strings.EqualFold(knownString(prev.Format), knownString(plan.Format)) || !copyFormatArgumentValue(plan.FormatArgument, knownString(plan.Format), knownString(prev.FormatArgument)).Equal(plan.FormatArgument)
}

// templateReplaceStatements renders CREATE OR REPLACE TEMPLATE and, when an owner is known, restores it, because the
// replacing statement may leave the template owned by the provider's user.
func templateReplaceStatements(plan templateModel) []string {
	statement, _ := templateDefinitionStatement("CREATE OR REPLACE TEMPLATE", plan) // alterTemplateStatements validated plan.

	statements := []string{statement}
	if knownString(plan.Owner) != "" {
		statements = append(statements, templateOwnerStatement(plan))
	}

	return statements
}

// templateParameterStatements renders one ALTER TEMPLATE per changed parameter: DROP for a removed one, which the
// command allows only one at a time, SET for a new value of a parameter that keeps an argument, and ADD otherwise.
// A parameter whose argument appears or disappears, such as ACCEPTINVCHARS gaining a replacement character, is
// dropped and added again, because SET applies only to parameters that already have a value.
func templateParameterStatements(prev, plan templateModel) []string {
	// alterTemplateStatements validated plan; prior state passed the same validation or came from the catalog.
	before, _ := copyCanonicalParameters(knownMap(prev.Parameters), false)
	after, _ := copyCanonicalParameters(knownMap(plan.Parameters), false)
	relation, _ := templateRelation(plan.Schema.ValueString(), plan.Name.ValueString())
	alter := sqlclient.Stmt("ALTER TEMPLATE").Append(relation)

	var statements []string

	for _, name := range slices.Sorted(maps.Keys(before)) {
		value, kept := after[name]

		keyword, parameter, _ := copyParameterKeyword(name, false)
		switch {
		case !kept:
			statements = append(statements, alter.Kw("DROP", keyword).String())
		case value == before[name]:
		case before[name] != "" && value != "":
			set, err := copyArgumentFragment(alter.Kw("SET", keyword, "TO"), parameter, value)
			if err == nil {
				statements = append(statements, set.String())
			}
		default:
			statements = append(statements, alter.Kw("DROP", keyword).String())

			added, err := copyParameterFragment(name, value, false)
			if err == nil {
				statements = append(statements, alter.Kw("ADD").Append(added).String())
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(after)) {
		if _, existed := before[name]; existed {
			continue
		}

		added, err := copyParameterFragment(name, after[name], false)
		if err == nil {
			statements = append(statements, alter.Kw("ADD").Append(added).String())
		}
	}

	return statements
}

// templateAlterSteps update a template in place: the rename first, so later statements use the new name, then the
// definition, then the owner.
var templateAlterSteps = []alterStep[templateModel]{
	{
		attribute: "name",
		value:     func(data templateModel) attr.Value { return data.Name },
		render: func(prev, plan templateModel) []string {
			relation, _ := templateRelation(plan.Schema.ValueString(), prev.Name.ValueString())
			return []string{sqlclient.Stmt("ALTER TEMPLATE").Append(relation).KwIdent("RENAME TO", plan.Name.ValueString()).String()}
		},
	},
	{
		attribute: "format",
		value:     func(data templateModel) attr.Value { return data.Format },
		render: func(prev, plan templateModel) []string {
			if !strings.EqualFold(knownString(prev.Format), knownString(plan.Format)) {
				return templateReplaceStatements(plan)
			}

			return nil
		},
	},
	{
		attribute: "format_argument",
		value:     func(data templateModel) attr.Value { return data.FormatArgument },
		render: func(prev, plan templateModel) []string {
			if strings.EqualFold(knownString(prev.Format), knownString(plan.Format)) && templateReplaced(prev, plan) {
				return templateReplaceStatements(plan)
			}

			return nil
		},
	},
	{
		attribute: "parameters",
		value:     func(data templateModel) attr.Value { return data.Parameters },
		render: func(prev, plan templateModel) []string {
			if templateReplaced(prev, plan) {
				return nil
			}

			return templateParameterStatements(prev, plan)
		},
	},
	{
		attribute: "owner",
		value:     func(data templateModel) attr.Value { return data.Owner },
		render: func(prev, plan templateModel) []string {
			if knownString(plan.Owner) == "" || templateReplaced(prev, plan) {
				return nil
			}

			return []string{templateOwnerStatement(plan)}
		},
	},
}

// alterTemplateStatements renders the in-place changes from prev to plan.
func alterTemplateStatements(prev, plan templateModel) ([]string, error) {
	if err := templateValidate(plan); err != nil {
		return nil, err
	}

	return alterStatements(prev, plan, templateAlterSteps), nil
}

// dropTemplateStatement renders DROP TEMPLATE, which has neither IF EXISTS nor CASCADE.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_TEMPLATE.html
func dropTemplateStatement(data templateModel) (string, error) {
	relation, err := templateRelation(data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		return "", err
	}

	return sqlclient.Stmt("DROP TEMPLATE").Append(relation).String(), nil
}

// templateLikePattern turns a name into a SHOW TEMPLATES LIKE pattern. The backslash is LIKE's escape character, so it
// becomes the single-character wildcard; _ and % stay wildcards. The pattern can therefore match more names than
// the one wanted, and callers compare names exactly.
func templateLikePattern(name string) string {
	return strings.ReplaceAll(name, `\`, "_")
}

// showTemplatesStatement renders SHOW TEMPLATES FROM SCHEMA, which lists a schema's templates with their owner. The
// database qualifier is omitted when database is empty, so the statement reads the connected database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_TEMPLATES.html
func showTemplatesStatement(database, schema, name string) string {
	return sqlclient.Stmt("SHOW TEMPLATES FROM SCHEMA").Qualified(database, schema).OptLit("LIKE", templateLikePattern(name)).String()
}

// showTemplateStatement renders SHOW TEMPLATE, which prints the template as one CREATE OR REPLACE TEMPLATE statement.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_TEMPLATE.html
func showTemplateStatement(data templateModel) string {
	relation, _ := templateRelation(data.Schema.ValueString(), data.Name.ValueString())
	return sqlclient.Stmt("SHOW TEMPLATE").Append(relation).String()
}

// listTemplatesQuery uses documented row visibility rather than probing schemas the caller cannot use.
// https://docs.aws.amazon.com/redshift/latest/dg/SYS_REDSHIFT_TEMPLATE.html
func listTemplatesQuery(database, schema string) sqlclient.Query {
	query := sqlclient.Select("schema_name", "template_name", "u.usename AS template_owner").
		From("sys_redshift_template t JOIN pg_user u ON t.owner_id = u.usesysid").Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("template_type = 1")
	if schema != "" {
		query = query.Where("schema_name = :schema", sqlclient.Bind("schema", schema))
	}

	return query.OrderBy("schema_name, template_name")
}

// templateDefinitionText returns the statement of a SHOW TEMPLATE row. The column name is not documented, so a row
// with one column is read whatever its name, and otherwise the column holding a CREATE statement is used.
func templateDefinitionText(row sqlclient.Row) (string, error) {
	if len(row) == 1 {
		for _, value := range row {
			return value, nil
		}
	}

	for _, name := range slices.Sorted(maps.Keys(row)) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(row[name])), "CREATE ") {
			return row[name], nil
		}
	}

	return "", fmt.Errorf("SHOW TEMPLATE returned no template definition")
}

// parseTemplateDefinition parses SHOW TEMPLATE output, CREATE OR REPLACE TEMPLATE db.schema.name FOR COPY AS options,
// into the format and parameters. The name is not checked, because the caller selected the template by name.
func parseTemplateDefinition(text string) (copyDefinition, error) {
	tokens, err := sqlclient.Tokens(text)
	if err != nil {
		return copyDefinition{}, fmt.Errorf("unreadable template definition: %w", err)
	}

	word := func(i int, expected string) bool {
		return i < len(tokens) && tokens[i].Kind == sqlclient.TokenWord && strings.EqualFold(tokens[i].Text, expected)
	}

	i := 0
	if !word(i, "CREATE") {
		return copyDefinition{}, fmt.Errorf("template definition does not start with CREATE: %q", text)
	}

	i++
	if word(i, "OR") && word(i+1, "REPLACE") {
		i += 2
	}

	if !word(i, "TEMPLATE") {
		return copyDefinition{}, fmt.Errorf("template definition is not CREATE TEMPLATE: %q", text)
	}

	i++
	for i < len(tokens) && (!word(i, "FOR") || !word(i+1, "COPY")) {
		i++
	}

	if i == len(tokens) {
		return copyDefinition{}, fmt.Errorf("template definition is not FOR COPY: %q", text)
	}

	i += 2
	if word(i, "AS") {
		i++
	}

	definition, err := parseCopyOptions(tokens[i:])
	if err != nil {
		return copyDefinition{}, fmt.Errorf("template definition: %w", err)
	}

	return definition, nil
}
