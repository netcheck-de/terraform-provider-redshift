package provider

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalFunctionTypes are the canonical base types that Lambda UDFs accept as arguments and results; aliases
// reach them through sqlclient.TypeName.
// https://docs.aws.amazon.com/redshift/latest/dg/udf-creating-a-lambda-sql-udf.html
var externalFunctionTypes = []string{
	"smallint", "integer", "bigint", "numeric", "real", "double precision", "character", "character varying",
	"boolean", "date", "timestamp without time zone",
}

// externalFunctionVolatilities are the documented categories; IMMUTABLE is not supported for Lambda UDFs.
var externalFunctionVolatilities = []sqlclient.Keyword{"VOLATILE", "STABLE"}

// externalFunctionBatchUnits are the MAX_BATCH_SIZE units; Redshift reads a bare size as KB.
var externalFunctionBatchUnits = []sqlclient.Keyword{"KB", "MB"}

// Documented limits of the Lambda invocation options.
const (
	externalFunctionMaxArguments  = 32
	externalFunctionMaxBatchRows  = 2147483647
	externalFunctionMaxBatchSizeK = 5 * 1024
	externalFunctionMaxBatchSizeM = 5
)

// externalFunctionRolePattern matches one IAM role ARN of an IAM_ROLE value, which may chain several with commas.
var externalFunctionRolePattern = regexp.MustCompile(`^arn:aws[a-z-]*:iam::\d{12}:role/[\w+=,.@/-]+$`)

// externalFunctionType validates a Lambda UDF argument or result type and returns the spelling format_type()
// reports, keeping any length or precision as configured.
func externalFunctionType(value string) (sqlclient.Keyword, error) {
	name, err := sqlclient.TypeName(value)
	if err != nil {
		return "", err
	}
	if !slices.Contains(externalFunctionTypes, externalFunctionBaseType(name)) {
		return "", fmt.Errorf("data type %q is not supported by Lambda UDFs", value)
	}
	return name, nil
}

// externalFunctionBaseType drops the modifiers of a canonical type, because the catalog reports routine argument
// and result types without them.
func externalFunctionBaseType(name sqlclient.Keyword) string {
	base, _, _ := strings.Cut(string(name), "(")
	return base
}

// externalFunctionTypeList returns the configured argument types in declaration order, which is part of the
// function's identity, so unlike knownStrings it never sorts.
func externalFunctionTypeList(value types.List) []string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var names []string
	for _, element := range value.Elements() {
		if text, ok := element.(types.String); ok {
			names = append(names, text.ValueString())
		}
	}
	return names
}

// externalFunctionTypeValues converts argument types to a Terraform list.
func externalFunctionTypeValues(names []string) types.List {
	elements := make([]attr.Value, len(names))
	for i, name := range names {
		elements[i] = types.StringValue(name)
	}
	return types.ListValueMust(types.StringType, elements)
}

// externalFunctionDeclaredTypes validates the argument types and renders them as declared, with modifiers.
func externalFunctionDeclaredTypes(argumentTypes []string) (sqlclient.Keyword, error) {
	if len(argumentTypes) > externalFunctionMaxArguments {
		return "", fmt.Errorf("a function accepts at most %d arguments, got %d", externalFunctionMaxArguments, len(argumentTypes))
	}
	for i, argumentType := range argumentTypes {
		if _, err := externalFunctionType(argumentType); err != nil {
			return "", fmt.Errorf("argument %d: %w", i+1, err)
		}
	}
	return sqlclient.Signature(argumentTypes...)
}

// externalFunctionSignature returns the canonical argument types without modifiers, the form
// oidvectortypes() reports and the one that identifies the overload in ALTER, DROP, and import identities.
func externalFunctionSignature(argumentTypes []string) (sqlclient.Keyword, error) {
	if _, err := externalFunctionDeclaredTypes(argumentTypes); err != nil {
		return "", err
	}
	bases := make([]string, len(argumentTypes))
	for i, argumentType := range argumentTypes {
		name, _ := sqlclient.TypeName(argumentType) // Validated by externalFunctionDeclaredTypes.
		bases[i] = externalFunctionBaseType(name)
	}
	return sqlclient.Signature(bases...)
}

// externalFunctionSignatureTypes splits a canonical signature back into argument types; the separator cannot
// occur inside a type because the signature carries no modifiers.
func externalFunctionSignatureTypes(signature string) []string {
	if signature == "" {
		return []string{}
	}
	return strings.Split(signature, ", ")
}

// externalFunctionIAMRole renders IAM_ROLE default or the role ARNs as a literal, after checking each chained ARN.
func externalFunctionIAMRole(role string) (sqlclient.Statement, error) {
	if strings.EqualFold(role, "default") {
		return sqlclient.Kw("default"), nil
	}
	for arn := range strings.SplitSeq(role, ",") {
		if !externalFunctionRolePattern.MatchString(arn) {
			return sqlclient.Statement{}, fmt.Errorf("iam_role must be default or comma-separated IAM role ARNs without spaces, got %q", role)
		}
	}
	return sqlclient.Lit(role), nil
}

// externalFunctionBatchSize renders MAX_BATCH_SIZE within the documented 1 KB to 5 MB range.
func externalFunctionBatchSize(data externalFunctionModel) (sqlclient.Statement, error) {
	size := knownInt64(data.MaxBatchSize)
	unit, err := optOneOf(data.MaxBatchSizeUnit, externalFunctionBatchUnits...)
	switch {
	case err != nil:
		return sqlclient.Statement{}, fmt.Errorf("max_batch_size_unit: %w", err)
	case size == nil && unit != "":
		return sqlclient.Statement{}, fmt.Errorf("max_batch_size_unit requires max_batch_size")
	case size == nil:
		return sqlclient.Fragment(), nil
	}
	limit, unitName := int64(externalFunctionMaxBatchSizeK), "KB"
	if unit == "MB" {
		limit, unitName = externalFunctionMaxBatchSizeM, "MB"
	}
	if *size < 1 || *size > limit {
		return sqlclient.Statement{}, fmt.Errorf("max_batch_size must be between 1 KB and 5 MB, got %d %s", *size, unitName)
	}
	return sqlclient.Fragment().KwInt("MAX_BATCH_SIZE", *size).OptKw(unit), nil
}

// externalFunctionDefinition renders CREATE [OR REPLACE] EXTERNAL FUNCTION with every configured option.
// Options left unset are omitted so Redshift applies its documented defaults, and replacing restates them all
// because OR REPLACE redefines the whole function.
func externalFunctionDefinition(data externalFunctionModel, replace bool) (string, error) {
	argumentTypes, err := externalFunctionDeclaredTypes(externalFunctionTypeList(data.Arguments))
	if err != nil {
		return "", err
	}
	returnType, err := externalFunctionType(data.ReturnType.ValueString())
	if err != nil {
		return "", fmt.Errorf("return_type: %w", err)
	}
	volatility := sqlclient.Keyword("VOLATILE")
	if configured := knownString(data.Volatility); configured != "" {
		if volatility, err = sqlclient.OneOf(configured, externalFunctionVolatilities...); err != nil {
			return "", fmt.Errorf("volatility: %w", err)
		}
	}
	if data.LambdaFunction.ValueString() == "" {
		return "", fmt.Errorf("lambda_function must not be empty")
	}
	role, err := externalFunctionIAMRole(data.IAMRole.ValueString())
	if err != nil {
		return "", err
	}
	if timeout := knownInt64(data.RetryTimeout); timeout != nil && *timeout < 0 {
		return "", fmt.Errorf("retry_timeout must be at least 0 milliseconds, got %d", *timeout)
	}
	if rows := knownInt64(data.MaxBatchRows); rows != nil && (*rows < 1 || *rows > externalFunctionMaxBatchRows) {
		return "", fmt.Errorf("max_batch_rows must be between 1 and %d, got %d", externalFunctionMaxBatchRows, *rows)
	}
	batchSize, err := externalFunctionBatchSize(data)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("CREATE").If(replace, "OR REPLACE").Kw("EXTERNAL FUNCTION").
		Qualified(data.Schema.ValueString(), data.Name.ValueString()).Args(sqlclient.Kw(argumentTypes)).
		Kw("RETURNS", returnType, volatility).
		KwLit("LAMBDA", data.LambdaFunction.ValueString()).
		Kw("IAM_ROLE").Append(role).
		OptInt("RETRY_TIMEOUT", knownInt64(data.RetryTimeout)).
		OptInt("MAX_BATCH_ROWS", knownInt64(data.MaxBatchRows)).
		Append(batchSize)
	return statement.String(), statement.Err()
}

// createExternalFunctionStatement renders CREATE EXTERNAL FUNCTION without OR REPLACE, so creation fails instead
// of silently adopting an existing overload.
func createExternalFunctionStatement(data externalFunctionModel) (string, error) {
	return externalFunctionDefinition(data, false)
}

// externalFunctionTarget renders the function name with its canonical signature, as ALTER and DROP require.
func externalFunctionTarget(data externalFunctionModel) (sqlclient.Statement, error) {
	signature, err := externalFunctionSignature(externalFunctionTypeList(data.Arguments))
	if err != nil {
		return sqlclient.Statement{}, err
	}
	return sqlclient.Fragment().Qualified(data.Schema.ValueString(), data.Name.ValueString()).Args(sqlclient.Kw(signature)), nil
}

// externalFunctionOwnerStatement renders ALTER FUNCTION ... OWNER TO, which only a superuser may run.
func externalFunctionOwnerStatement(data externalFunctionModel) (string, error) {
	target, err := externalFunctionTarget(data)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("ALTER FUNCTION").Append(target).KwIdent("OWNER TO", data.Owner.ValueString())
	return statement.String(), statement.Err()
}

// createExternalFunctionStatements renders creation, followed by the owner change when an owner is configured.
func createExternalFunctionStatements(data externalFunctionModel) ([]string, error) {
	create, err := createExternalFunctionStatement(data)
	if err != nil {
		return nil, err
	}
	statements := []string{create}
	if knownString(data.Owner) != "" {
		owner, err := externalFunctionOwnerStatement(data)
		if err != nil {
			return nil, err
		}
		statements = append(statements, owner)
	}
	return statements, nil
}

// externalFunctionRestate renders the OR REPLACE statement for an alter step. alterExternalFunctionStatements
// validates the plan first, so the error cannot occur here.
func externalFunctionRestate(_, plan externalFunctionModel) []string {
	statement, _ := externalFunctionDefinition(plan, true)
	return []string{statement}
}

// externalFunctionRestateIfDeclared restates the definition only when the declared types change, not for a
// different spelling of the same type such as int for integer.
func externalFunctionRestateIfDeclared(declared func(externalFunctionModel) (sqlclient.Keyword, error)) func(prev, plan externalFunctionModel) []string {
	return func(prev, plan externalFunctionModel) []string {
		before, errBefore := declared(prev)
		after, _ := declared(plan)
		if errBefore == nil && before == after {
			return nil
		}
		return externalFunctionRestate(prev, plan)
	}
}

// externalFunctionDeclaredArguments returns the canonical declared argument types of a model.
func externalFunctionDeclaredArguments(data externalFunctionModel) (sqlclient.Keyword, error) {
	return externalFunctionDeclaredTypes(externalFunctionTypeList(data.Arguments))
}

// externalFunctionDeclaredReturn returns the canonical declared result type of a model.
func externalFunctionDeclaredReturn(data externalFunctionModel) (sqlclient.Keyword, error) {
	return externalFunctionType(data.ReturnType.ValueString())
}

// externalFunctionAlterSteps restate the definition with CREATE OR REPLACE, which redefines the overload instead of
// dropping it, for every option Redshift cannot alter on its own; only the owner has an ALTER FUNCTION form.
// A change of the argument or result base type replaces the function instead, because OR REPLACE requires the
// same set of data types.
var externalFunctionAlterSteps = []alterStep[externalFunctionModel]{
	{attribute: "arguments", value: func(data externalFunctionModel) attr.Value { return data.Arguments }, render: externalFunctionRestateIfDeclared(externalFunctionDeclaredArguments)},
	{attribute: "return_type", value: func(data externalFunctionModel) attr.Value { return data.ReturnType }, render: externalFunctionRestateIfDeclared(externalFunctionDeclaredReturn)},
	{attribute: "volatility", value: func(data externalFunctionModel) attr.Value { return data.Volatility }, render: externalFunctionRestate},
	{attribute: "lambda_function", value: func(data externalFunctionModel) attr.Value { return data.LambdaFunction }, render: externalFunctionRestate},
	{attribute: "iam_role", value: func(data externalFunctionModel) attr.Value { return data.IAMRole }, render: externalFunctionRestate},
	{attribute: "retry_timeout", value: func(data externalFunctionModel) attr.Value { return data.RetryTimeout }, render: externalFunctionRestate},
	{attribute: "max_batch_rows", value: func(data externalFunctionModel) attr.Value { return data.MaxBatchRows }, render: externalFunctionRestate},
	{attribute: "max_batch_size", value: func(data externalFunctionModel) attr.Value { return data.MaxBatchSize }, render: externalFunctionRestate},
	{attribute: "max_batch_size_unit", value: func(data externalFunctionModel) attr.Value { return data.MaxBatchSizeUnit }, render: externalFunctionRestate},
	{attribute: "owner", value: func(data externalFunctionModel) attr.Value { return data.Owner }, render: func(_, plan externalFunctionModel) []string {
		if knownString(plan.Owner) == "" {
			return nil
		}
		statement, _ := externalFunctionOwnerStatement(plan) // Validated by alterExternalFunctionStatements.
		return []string{statement}
	}},
}

// alterExternalFunctionStatements renders the in-place changes from prev to plan. Several changed options
// restate the same definition, so repeated statements collapse into one; the owner change runs last because
// it names the replaced function.
func alterExternalFunctionStatements(prev, plan externalFunctionModel) ([]string, error) {
	if _, err := externalFunctionDefinition(plan, true); err != nil {
		return nil, err
	}
	return slices.Compact(alterStatements(prev, plan, externalFunctionAlterSteps)), nil
}

// dropExternalFunctionStatement renders DROP FUNCTION without CASCADE, so dependent views keep the function.
func dropExternalFunctionStatement(data externalFunctionModel) (string, error) {
	target, err := externalFunctionTarget(data)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("DROP FUNCTION").Append(target)
	return statement.String(), statement.Err()
}

// externalFunctionColumns are the catalog columns read for an external function and for routine listings.
var externalFunctionColumns = []sqlclient.Keyword{
	"n.nspname AS schema_name", "p.proname AS routine_name", "oidvectortypes(p.proargtypes) AS arguments",
	"format_type(p.prorettype, NULL) AS return_type", "p.provolatile AS volatility", "p.prosecdef AS security_definer",
	"l.lanname AS language", "u.usename AS owner",
}

// externalFunctionSource joins a routine with its schema, language, and owner. The owner join is outer so a
// routine whose owner was dropped is still found and reported as incomplete.
const externalFunctionSource = "pg_namespace n ON p.pronamespace = n.oid JOIN pg_language l ON p.prolang = l.oid LEFT JOIN pg_user u ON p.proowner = u.usesysid"

// readExternalFunctionQuery reads one overload from pg_proc. A function without arguments is matched by its
// argument count, because the Data API rejects an empty signature binding.
func readExternalFunctionQuery(data externalFunctionModel) (sqlclient.Query, error) {
	signature, err := externalFunctionSignature(externalFunctionTypeList(data.Arguments))
	if err != nil {
		return sqlclient.Query{}, err
	}
	return sqlclient.Select(externalFunctionColumns...).From("pg_proc p JOIN "+externalFunctionSource).
		Where("n.nspname = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("p.proname = :name", sqlclient.Bind("name", data.Name.ValueString())).
		WhereEither(signature == "", "p.pronargs = 0", "oidvectortypes(p.proargtypes) = :arguments", sqlclient.Bind("arguments", string(signature))), nil
}

// externalFunctionVolatility maps pg_proc.provolatile to its SQL keyword.
func externalFunctionVolatility(code string) (string, error) {
	switch code {
	case "v":
		return "VOLATILE", nil
	case "s":
		return "STABLE", nil
	case "i":
		return "IMMUTABLE", nil
	}
	return "", fmt.Errorf("unknown routine volatility %q", code)
}
