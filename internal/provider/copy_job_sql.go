package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// copyJobTableType is the object type of the table block.
var copyJobTableType = types.ObjectType{AttrTypes: map[string]attr.Type{"schema": types.StringType, "name": types.StringType, "columns": types.ListType{ElemType: types.StringType}}}

// copyJobName admits the names COPY JOB accepts unquoted. The command documents no quoting and forbids hyphens, and
// Redshift folds unquoted names to lowercase, so lowercase letters, digits, and underscores keep the configured name
// equal to the one SYS_COPY_JOB reports.
var copyJobName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,126}$`)

// copyJobSourcePattern and copyJobRolePattern give plan-time feedback on the S3 prefix and the role ARN.
var (
	copyJobSourcePattern = regexp.MustCompile(`^s3://.+`)
	copyJobRolePattern   = regexp.MustCompile(`^arn:[^:]+:iam::`)
)

// copyJobRoleLength is the width of SYS_COPY_JOB.iam_role, which cuts longer role ARNs.
const copyJobRoleLength = 128

// copyJobTable is the target of the COPY command.
type copyJobTable struct {
	// schema contains the table.
	schema string
	// name is the table name.
	name string
	// columns optionally lists the loaded columns in file order.
	columns []string
}

// copyJobTarget reads the table block; a null or unknown block yields an empty target that validation rejects.
func copyJobTarget(table types.Object) copyJobTable {
	if table.IsNull() || table.IsUnknown() {
		return copyJobTable{}
	}
	target := copyJobTable{schema: objectString(table, "schema"), name: objectString(table, "name")}
	if columns, ok := table.Attributes()["columns"].(types.List); ok && !columns.IsNull() && !columns.IsUnknown() {
		for _, column := range columns.Elements() {
			target.columns = append(target.columns, column.(types.String).ValueString())
		}
	}
	return target
}

// copyJobValidate checks what COPY JOB CREATE needs before any SQL runs. COPY JOB rejects MAXERROR,
// IGNOREALLERRORS, a manifest, and key-based credentials, and needs an explicit IAM role because the warehouse's
// default role is not supported.
// https://docs.aws.amazon.com/redshift/latest/dg/r_COPY-JOB.html
func copyJobValidate(data copyJobModel) error {
	if !copyJobName.MatchString(data.Name.ValueString()) {
		return fmt.Errorf("name %q must be 1 to 127 lowercase letters, digits, or underscores, starting with a letter or underscore; COPY JOB names cannot contain hyphens", data.Name.ValueString())
	}
	target := copyJobTarget(data.Table)
	if target.schema == "" || target.name == "" {
		return fmt.Errorf("the table block needs a nonempty schema and name")
	}
	for _, column := range target.columns {
		if column == "" {
			return fmt.Errorf("table.columns must not contain empty names")
		}
	}
	if !copyJobSourcePattern.MatchString(data.Source.ValueString()) {
		return fmt.Errorf("source %q must be an s3:// prefix", data.Source.ValueString())
	}
	if !copyJobRolePattern.MatchString(data.IAMRole.ValueString()) {
		return fmt.Errorf("iam_role %q must be an IAM role ARN; COPY JOB does not support the default role", data.IAMRole.ValueString())
	}
	if _, err := copyCheckFormat(knownString(data.Format), knownString(data.FormatArgument)); err != nil {
		return err
	}
	_, err := copyCanonicalParameters(knownMap(data.Parameters), true)
	return err
}

// copyJobKeyword renders the job name, which copyJobValidate restricted to unquoted identifier characters.
func copyJobKeyword(name string) sqlclient.Keyword {
	return sqlclient.Keyword(name) //sql:trusted copyJobName admits only lowercase letters, digits, and underscores.
}

// copyJobAuto renders AUTO ON or AUTO OFF; an unknown value takes the documented default, ON.
func copyJobAuto(statement sqlclient.Statement, auto types.Bool) sqlclient.Statement {
	return statement.Kw("AUTO").Toggle(auto.IsNull() || auto.IsUnknown() || auto.ValueBool(), "ON", "OFF")
}

// createCopyJobStatement renders COPY table [(columns)] FROM 's3://...' IAM_ROLE '...' options JOB CREATE name AUTO.
// The table is schema-qualified in the job's database. AUTO is always explicit, so the stored job does not depend
// on the documented default.
// https://docs.aws.amazon.com/redshift/latest/dg/r_COPY-JOB.html
func createCopyJobStatement(data copyJobModel) (string, error) {
	if err := copyJobValidate(data); err != nil {
		return "", err
	}
	target := copyJobTarget(data.Table)
	format, err := copyFormatFragment(knownString(data.Format), knownString(data.FormatArgument))
	if err != nil {
		return "", err
	}
	parameters, err := copyParameterFragments(knownMap(data.Parameters), true)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("COPY").Qualified(target.schema, target.name).
		When(len(target.columns) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Paren(sqlclient.Fragment().Idents(target.columns...))
		}).
		KwLit("FROM", data.Source.ValueString()).KwLit("IAM_ROLE", data.IAMRole.ValueString()).
		Append(format).Append(parameters...).Kw("JOB CREATE", copyJobKeyword(data.Name.ValueString()))
	statement = copyJobAuto(statement, data.Auto)
	return statement.String(), statement.Err()
}

// copyJobAlterSteps update a job in place: COPY JOB ALTER changes only AUTO, so every other change replaces the job.
var copyJobAlterSteps = []alterStep[copyJobModel]{{
	attribute: "auto",
	value:     func(data copyJobModel) attr.Value { return data.Auto },
	render: func(_, plan copyJobModel) []string {
		return []string{copyJobAuto(sqlclient.Stmt("COPY JOB ALTER").Kw(copyJobKeyword(plan.Name.ValueString())), plan.Auto).String()}
	},
}}

// alterCopyJobStatements renders the in-place changes from prev to plan.
func alterCopyJobStatements(prev, plan copyJobModel) ([]string, error) {
	if !copyJobName.MatchString(plan.Name.ValueString()) {
		return nil, fmt.Errorf("invalid copy job name %q", plan.Name.ValueString())
	}
	return alterStatements(prev, plan, copyJobAlterSteps), nil
}

// dropCopyJobStatement renders COPY JOB DROP; files the job loaded stay in the table.
func dropCopyJobStatement(data copyJobModel) (string, error) {
	if !copyJobName.MatchString(data.Name.ValueString()) {
		return "", fmt.Errorf("invalid copy job name %q", data.Name.ValueString())
	}
	return sqlclient.Stmt("COPY JOB DROP").Kw(copyJobKeyword(data.Name.ValueString())).String(), nil
}

// copyJobCatalogColumns are the SYS_COPY_JOB columns refresh reads. job_text holds the COPY parameters but is cut at
// 256 characters in an undocumented form, so the definition is not read back.
// https://docs.aws.amazon.com/redshift/latest/dg/SYS_COPY_JOB.html
var copyJobCatalogColumns = []sqlclient.Keyword{"job_name", "iam_role", "is_auto"}

// readCopyJobQuery reads one job of the connected database. SYS_COPY_JOB shows superusers every job and other users
// only their own.
func readCopyJobQuery(name string) sqlclient.Query {
	return sqlclient.Select(copyJobCatalogColumns...).From("sys_copy_job").Where("job_name = :name", sqlclient.Bind("name", name))
}

// listCopyJobsQuery lists the visible jobs by name.
func listCopyJobsQuery() sqlclient.Query {
	return sqlclient.Select(copyJobCatalogColumns...).From("sys_copy_job").OrderBy("job_name")
}

// copyJobAutoValue reads is_auto, an integer that is 1 for an automatic job.
func copyJobAutoValue(text string) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(text)) {
	case "1", "t", "true":
		return true, nil
	case "0", "f", "false":
		return false, nil
	}
	return false, fmt.Errorf("unexpected SYS_COPY_JOB.is_auto %q", text)
}

// copyJobRoleValue keeps the configured role while the catalog reports it, also when SYS_COPY_JOB cut a role ARN longer
// than its 128-character column, and otherwise reports the catalog role.
func copyJobRoleValue(configured types.String, catalog string) types.String {
	catalog = strings.TrimSpace(catalog)
	role := knownString(configured)
	if role == catalog || len(role) > copyJobRoleLength && role[:copyJobRoleLength] == catalog {
		return configured
	}
	return types.StringValue(catalog)
}
