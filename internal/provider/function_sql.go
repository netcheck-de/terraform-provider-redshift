package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// errFunctionPython rejects Python UDFs, which AWS stops supporting; the provider never deploys new ones.
var errFunctionPython = errors.New("language plpythonu is not supported: Amazon Redshift ends support for Python UDFs after June 30, 2026; " +
	"rewrite the function as a SQL UDF (LANGUAGE sql) or a Lambda UDF")

// functionSpec is the validated definition of a SQL UDF, rendered by the function statements.
type functionSpec struct {
	// schema and name qualify the function.
	schema, name string
	// arguments are the canonical input types with modifiers, as CREATE declares them.
	arguments []sqlclient.Keyword
	// signature is the bare input types that identify the overload in ALTER, DROP and the catalog.
	signature sqlclient.Keyword
	// returns is the canonical return type.
	returns sqlclient.Keyword
	// volatility is VOLATILE, STABLE or IMMUTABLE.
	volatility sqlclient.Keyword
	// body is the SELECT clause, dollar-quoted verbatim.
	body string
}

// functionSignature returns the canonical overload signature of the configured or stored arguments.
func functionSignature(data functionModel) (sqlclient.Keyword, error) {
	return routineSignature(routineStrings(data.Arguments))
}

// functionLanguage checks the language before anything else, so a Python UDF gets the end-of-support explanation
// rather than a type error. sql must be lowercase: state reports pg_language.lanname, and another spelling would
// make every apply inconsistent with the plan and every import replace the function.
func functionLanguage(data functionModel) error {
	language := knownString(data.Language)
	if strings.EqualFold(language, "plpythonu") {
		return errFunctionPython
	}
	if language != "" && language != "sql" {
		return fmt.Errorf("language %q is not supported; only sql, in lowercase, is", language)
	}
	return nil
}

// newFunctionSpec validates a function configuration; ValidateConfig and Create share it.
func newFunctionSpec(data functionModel) (functionSpec, error) {
	if err := functionLanguage(data); err != nil {
		return functionSpec{}, err
	}
	spec := functionSpec{schema: knownString(data.Schema), name: knownString(data.Name), body: knownString(data.Body)}
	if spec.schema == "" || spec.name == "" {
		return functionSpec{}, errors.New("schema and name must be nonempty")
	}
	arguments := routineStrings(data.Arguments)
	if len(arguments) > routineMaxArguments {
		return functionSpec{}, fmt.Errorf("a function accepts at most %d arguments, got %d", routineMaxArguments, len(arguments))
	}
	for i, argument := range arguments {
		name, err := routineType(argument)
		if err == nil {
			err = routineRejectedType(routineBaseType(name), "function")
		}
		if err != nil {
			return functionSpec{}, fmt.Errorf("argument %d: %w", i+1, err)
		}
		spec.arguments = append(spec.arguments, name)
	}
	var err error
	if spec.signature, err = routineSignature(arguments); err != nil {
		return functionSpec{}, err
	}
	if spec.returns, err = routineType(knownString(data.ReturnType)); err == nil {
		err = routineRejectedType(routineBaseType(spec.returns), "function")
	}
	if err != nil {
		return functionSpec{}, fmt.Errorf("return_type: %w", err)
	}
	volatility := knownString(data.Volatility)
	if volatility == "" {
		volatility = "VOLATILE"
	}
	if spec.volatility, err = sqlclient.OneOf(volatility, "VOLATILE", "STABLE", "IMMUTABLE"); err != nil {
		return functionSpec{}, fmt.Errorf("volatility: %w", err)
	}
	if strings.TrimSpace(spec.body) == "" {
		return functionSpec{}, errors.New("body must contain a SELECT clause")
	}
	return spec, nil
}

// functionArguments renders the declared input types; SQL UDF arguments have no names and are referenced as $1, $2.
func functionArguments(argumentTypes []sqlclient.Keyword) []sqlclient.Statement {
	items := make([]sqlclient.Statement, len(argumentTypes))
	for i, name := range argumentTypes {
		items[i] = sqlclient.Kw(name)
	}
	return items
}

// createFunctionStatement renders CREATE [OR REPLACE] FUNCTION. Create never replaces, so an existing overload is
// reported instead of adopted; Update replaces to change the body or volatility of the same signature.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_FUNCTION.html
func createFunctionStatement(spec functionSpec, replace bool) string {
	return sqlclient.Stmt("CREATE").If(replace, "OR REPLACE").Kw("FUNCTION").Qualified(spec.schema, spec.name).
		Args(functionArguments(spec.arguments)...).Kw("RETURNS").Kw(spec.returns).Kw(spec.volatility).
		Kw("AS").Body(spec.body).Kw("LANGUAGE sql").String()
}

// functionTarget renders the function and its input types, as ALTER FUNCTION and DROP FUNCTION require.
func functionTarget(verb sqlclient.Keyword, schema, name string, signature sqlclient.Keyword) sqlclient.Statement {
	return sqlclient.Stmt(verb).Qualified(schema, name).Args(sqlclient.Kw(signature))
}

// functionAlter starts ALTER FUNCTION for the stored overload.
func functionAlter(data functionModel) sqlclient.Statement {
	signature, _ := functionSignature(data) // Validated before any statement renders.
	return functionTarget("ALTER FUNCTION", data.Schema.ValueString(), data.Name.ValueString(), signature)
}

// functionAlterSteps changes the definition with CREATE OR REPLACE and the owner with ALTER FUNCTION ... OWNER TO.
// arguments and return_type replace the function unless only their spelling changes, which needs no statement, or
// modifiers are added to a type stored without them, as after an import, which CREATE OR REPLACE restates.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_FUNCTION.html
func functionAlterSteps(spec functionSpec) []alterStep[functionModel] {
	steps := routineRedefinitionSteps(createFunctionStatement(spec, true),
		routineDefinitionAttribute[functionModel]{name: "body", value: func(data functionModel) attr.Value { return data.Body }},
		routineDefinitionAttribute[functionModel]{name: "volatility", value: func(data functionModel) attr.Value { return data.Volatility }},
		routineDefinitionAttribute[functionModel]{name: "arguments", value: func(data functionModel) attr.Value {
			return routineDeclaredTypes(data.Arguments.IsUnknown(), routineStrings(data.Arguments)...)
		}},
		routineDefinitionAttribute[functionModel]{name: "return_type", value: func(data functionModel) attr.Value {
			return routineDeclaredTypes(data.ReturnType.IsUnknown(), data.ReturnType.ValueString())
		}},
	)
	return append(steps, functionOwnerStep)
}

// functionOwnerStep renders ALTER FUNCTION ... OWNER TO for a configured owner.
var functionOwnerStep = routineOwnerStep(func(data functionModel) types.String { return data.Owner }, functionAlter)

// alterFunctionStatements renders the in-place changes from prev to plan in execution order: the redefinition
// first, because CREATE OR REPLACE requires the current owner or a superuser.
func alterFunctionStatements(prev, plan functionModel) ([]string, error) {
	spec, err := newFunctionSpec(plan)
	if err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, functionAlterSteps(spec)), nil
}

// dropFunctionStatement renders DROP FUNCTION without CASCADE, so views that depend on it keep it in place.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_FUNCTION.html
func dropFunctionStatement(data functionModel) (string, error) {
	signature, err := functionSignature(data)
	if err != nil {
		return "", err
	}
	return functionTarget("DROP FUNCTION", data.Schema.ValueString(), data.Name.ValueString(), signature).String(), nil
}

// readFunctionQuery reads one regular function overload. pg_proc_info adds prokind to pg_proc, which separates
// functions ('f') from stored procedures ('p'); oidvectortypes reports the bare input types of the overload.
// https://docs.aws.amazon.com/redshift/latest/dg/r_PG_PROC_INFO.html
func readFunctionQuery(schema, name string, signature sqlclient.Keyword) sqlclient.Query {
	return sqlclient.Select("p.proname AS function_name", "u.usename AS owner", "l.lanname AS language",
		"p.provolatile AS volatility", "format_type(p.prorettype, NULL) AS return_type",
		"oidvectortypes(p.proargtypes) AS arguments", "p.prosrc AS body").
		From("pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_language l ON l.oid = p.prolang JOIN pg_user u ON u.usesysid = p.proowner").
		Where("n.nspname = :schema", sqlclient.Bind("schema", schema)).
		Where("p.proname = :name", sqlclient.Bind("name", name)).
		Where("p.prokind = 'f'").
		WhereEither(signature == "", "p.pronargs = 0", "oidvectortypes(p.proargtypes) = :arguments", sqlclient.Bind("arguments", string(signature)))
}
