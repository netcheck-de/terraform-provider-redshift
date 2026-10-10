package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// procedureParameterName accepts configuration parameter names, which SET emits unquoted.
var procedureParameterName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// procedureArgumentSpec is one validated procedure argument.
type procedureArgumentSpec struct {
	// name is optional; Redshift identifies a procedure by its input types only.
	name string
	// mode is IN, OUT, or INOUT.
	mode sqlclient.Keyword
	// dataType is the canonical type with modifiers.
	dataType sqlclient.Keyword
}

// procedureSetting is one SET clause.
type procedureSetting struct {
	// parameter is the checked configuration parameter name.
	parameter sqlclient.Keyword
	// values are rendered as a comma-separated list of string literals.
	values []string
}

// procedureListParameter names the parameter whose value is a list of schema names. Redshift reads each quoted
// literal of it as one schema name, so 'analytics, public' would name a single schema; the elements are rendered
// as separate literals instead.
// https://docs.aws.amazon.com/redshift/latest/dg/r_search_path.html
const procedureListParameter = "search_path"

// procedureSettingValues splits a search_path value into its trimmed schema names and keeps any other value whole.
func procedureSettingValues(parameter, value string) ([]string, error) {
	if !strings.EqualFold(parameter, procedureListParameter) {
		return []string{value}, nil
	}
	names := strings.Split(value, ",")
	for i := range names {
		if names[i] = strings.TrimSpace(names[i]); names[i] == "" {
			return nil, fmt.Errorf("configuration %s must be a comma-separated list of nonempty schema names, got %q", parameter, value)
		}
	}
	return names, nil
}

// procedureSpec is the validated definition of a PL/pgSQL stored procedure.
type procedureSpec struct {
	// schema and name qualify the procedure.
	schema, name string
	// arguments are declared in order with their modes.
	arguments []procedureArgumentSpec
	// signature is the bare IN and INOUT types that identify the procedure in ALTER, DROP, SHOW, and the catalog.
	signature sqlclient.Keyword
	// body is the PL/pgSQL block, dollar-quoted verbatim.
	body string
	// security is INVOKER or DEFINER.
	security sqlclient.Keyword
	// nonatomic selects automatic commits inside the procedure.
	nonatomic bool
	// settings are the SET clauses in name order.
	settings []procedureSetting
}

// procedureArgumentModel is one argument block.
type procedureArgumentModel struct {
	// Name is the optional argument name.
	Name types.String `tfsdk:"name"`
	// Mode is IN (the default when null), OUT, or INOUT.
	Mode types.String `tfsdk:"mode"`
	// Type is the argument data type.
	Type types.String `tfsdk:"type"`
}

// procedureArgumentTypes are the attribute types of one argument element.
var procedureArgumentTypes = map[string]attr.Type{"name": types.StringType, "mode": types.StringType, "type": types.StringType}

// procedureArguments returns the known argument elements in order.
func procedureArguments(value types.List) []procedureArgumentModel {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var arguments []procedureArgumentModel
	// Elements of a list of this element type always convert; unknown nested values read as unknown fields.
	_ = value.ElementsAs(context.Background(), &arguments, false)
	return arguments
}

// procedureArgumentList builds the argument blocks; none is null, as the framework reads an absent block.
func procedureArgumentList(arguments []procedureArgumentModel) types.List {
	elementType := types.ObjectType{AttrTypes: procedureArgumentTypes}
	if len(arguments) == 0 {
		return types.ListNull(elementType)
	}
	values := make([]attr.Value, len(arguments))
	for i, argument := range arguments {
		values[i] = types.ObjectValueMust(procedureArgumentTypes, map[string]attr.Value{"name": argument.Name, "mode": argument.Mode, "type": argument.Type})
	}
	return types.ListValueMust(elementType, values)
}

// procedureMode returns an argument's mode with null meaning IN.
func procedureMode(argument procedureArgumentModel) string {
	if mode := knownString(argument.Mode); mode != "" {
		return strings.ToUpper(mode)
	}
	return "IN"
}

// procedureInput reports whether an argument is part of the signature: OUT arguments are not.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_PROCEDURE.html
func procedureInput(mode string) bool {
	return mode != "OUT"
}

// procedureSignature returns the canonical bare types of the IN and INOUT arguments.
func procedureSignature(arguments []procedureArgumentModel) (sqlclient.Keyword, error) {
	var inputs []string
	for _, argument := range arguments {
		if procedureInput(procedureMode(argument)) {
			inputs = append(inputs, argument.Type.ValueString())
		}
	}
	return routineSignature(inputs)
}

// procedureArgumentsEquivalent reports whether the planned arguments after keep the procedure of the stored ones
// before: equal modes, types that routineTypeReplaces accepts, and names equal ignoring case, as Redshift folds
// them by default.
func procedureArgumentsEquivalent(before, after []procedureArgumentModel) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if procedureMode(before[i]) != procedureMode(after[i]) || !strings.EqualFold(before[i].Name.ValueString(), after[i].Name.ValueString()) ||
			routineTypeReplaces(before[i].Type.ValueString(), after[i].Type.ValueString()) {
			return false
		}
	}
	return true
}

// procedureDeclaredTypes returns the canonical declared argument types as one comparable value.
func procedureDeclaredTypes(data procedureModel) attr.Value {
	arguments := procedureArguments(data.Arguments)
	dataTypes := make([]string, len(arguments))
	for i, argument := range arguments {
		dataTypes[i] = argument.Type.ValueString()
	}
	return routineDeclaredTypes(data.Arguments.IsUnknown(), dataTypes...)
}

// newProcedureSpec validates a procedure configuration; ValidateConfig and Create share it.
func newProcedureSpec(data procedureModel) (procedureSpec, error) {
	spec := procedureSpec{schema: knownString(data.Schema), name: knownString(data.Name), body: knownString(data.Body), nonatomic: data.Nonatomic.ValueBool()}
	if spec.schema == "" || spec.name == "" {
		return procedureSpec{}, errors.New("schema and name must be nonempty")
	}
	inputs, outputs := 0, 0
	for i, argument := range procedureArguments(data.Arguments) {
		mode, err := sqlclient.OneOf(procedureMode(argument), "IN", "OUT", "INOUT")
		if err != nil {
			return procedureSpec{}, fmt.Errorf("argument %d mode: %w", i+1, err)
		}
		dataType, err := routineType(argument.Type.ValueString())
		if err == nil {
			err = routineRejectedType(routineBaseType(dataType), "procedure")
		}
		if err != nil {
			return procedureSpec{}, fmt.Errorf("argument %d: %w", i+1, err)
		}
		if !argument.Name.IsNull() && argument.Name.ValueString() == "" {
			return procedureSpec{}, fmt.Errorf("argument %d: name must be nonempty when set", i+1)
		}
		if mode != "OUT" {
			inputs++
		}
		if mode != "IN" {
			outputs++
		}
		spec.arguments = append(spec.arguments, procedureArgumentSpec{name: argument.Name.ValueString(), mode: mode, dataType: dataType})
	}
	if inputs > routineMaxArguments || outputs > routineMaxArguments {
		return procedureSpec{}, fmt.Errorf("a procedure accepts at most %d input and %d output arguments", routineMaxArguments, routineMaxArguments)
	}
	var err error
	if spec.signature, err = procedureSignature(procedureArguments(data.Arguments)); err != nil {
		return procedureSpec{}, err
	}
	security := knownString(data.Security)
	if security == "" {
		security = "INVOKER"
	}
	if spec.security, err = sqlclient.OneOf(security, "INVOKER", "DEFINER"); err != nil {
		return procedureSpec{}, fmt.Errorf("security: %w", err)
	}
	settings := knownMap(data.Configuration)
	for _, parameter := range slices.Sorted(maps.Keys(settings)) {
		if !procedureParameterName.MatchString(parameter) {
			return procedureSpec{}, fmt.Errorf("configuration parameter %q is not a valid parameter name", parameter)
		}
		values, err := procedureSettingValues(parameter, settings[parameter])
		if err != nil {
			return procedureSpec{}, err
		}
		//sql:trusted the name matched procedureParameterName, which admits only unquoted identifier characters.
		spec.settings = append(spec.settings, procedureSetting{parameter: sqlclient.Keyword(parameter), values: values})
	}
	// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_PROCEDURE.html: NONATOMIC excludes both options.
	if spec.nonatomic && spec.security == "DEFINER" {
		return procedureSpec{}, errors.New("SECURITY DEFINER is not supported for a NONATOMIC procedure")
	}
	if spec.nonatomic && len(spec.settings) > 0 {
		return procedureSpec{}, errors.New("SET configuration parameters are not supported for a NONATOMIC procedure")
	}
	if strings.TrimSpace(spec.body) == "" {
		return procedureSpec{}, errors.New("body must contain a PL/pgSQL block")
	}
	return spec, nil
}

// createProcedureStatement renders CREATE [OR REPLACE] PROCEDURE in the documented clause order. Create never
// replaces, so an existing procedure is reported instead of adopted; Update replaces to change the definition.
// SECURITY is spelled out unless NONATOMIC, which rejects SECURITY DEFINER and keeps the INVOKER default.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_PROCEDURE.html
func createProcedureStatement(spec procedureSpec, replace bool) string {
	arguments := make([]sqlclient.Statement, len(spec.arguments))
	for i, argument := range spec.arguments {
		item := sqlclient.Fragment()
		if argument.name != "" {
			item = item.Ident(argument.name)
		}
		arguments[i] = item.Kw(argument.mode).Kw(argument.dataType)
	}
	statement := sqlclient.Stmt("CREATE").If(replace, "OR REPLACE").Kw("PROCEDURE").Qualified(spec.schema, spec.name).
		Args(arguments...).If(spec.nonatomic, "NONATOMIC").Kw("AS").Body(spec.body).Kw("LANGUAGE plpgsql").
		When(!spec.nonatomic, func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("SECURITY").Kw(spec.security) })
	for _, setting := range spec.settings {
		values := make([]sqlclient.Statement, len(setting.values))
		for i, value := range setting.values {
			values[i] = sqlclient.Fragment().Lit(value)
		}
		statement = statement.Kw("SET").Kw(setting.parameter).Kw("TO").List(values...)
	}
	return statement.String()
}

// procedureTarget renders the procedure and its input types, as ALTER PROCEDURE, DROP PROCEDURE, and SHOW
// PARAMETERS accept them.
func procedureTarget(verb sqlclient.Keyword, schema, name string, signature sqlclient.Keyword) sqlclient.Statement {
	return sqlclient.Stmt(verb).Qualified(schema, name).Args(sqlclient.Kw(signature))
}

// procedureAlter starts ALTER PROCEDURE for the stored procedure.
func procedureAlter(data procedureModel) sqlclient.Statement {
	signature, _ := procedureSignature(procedureArguments(data.Arguments)) // Validated before any statement renders.
	return procedureTarget("ALTER PROCEDURE", data.Schema.ValueString(), data.Name.ValueString(), signature)
}

// procedureOwnerStep renders ALTER PROCEDURE ... OWNER TO for a configured owner.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_PROCEDURE.html
var procedureOwnerStep = routineOwnerStep(func(data procedureModel) types.String { return data.Owner }, procedureAlter)

// procedureAlterSteps changes the definition with CREATE OR REPLACE and the owner with ALTER PROCEDURE. Argument
// blocks replace the procedure unless only their spelling changes, which needs no statement, or modifiers are added
// to a type stored without them, as after an import, which CREATE OR REPLACE restates.
func procedureAlterSteps(spec procedureSpec) []alterStep[procedureModel] {
	steps := routineRedefinitionSteps(createProcedureStatement(spec, true),
		routineDefinitionAttribute[procedureModel]{name: "argument", value: procedureDeclaredTypes},
		routineDefinitionAttribute[procedureModel]{name: "body", value: func(data procedureModel) attr.Value { return data.Body }},
		routineDefinitionAttribute[procedureModel]{name: "security", value: func(data procedureModel) attr.Value { return data.Security }},
		routineDefinitionAttribute[procedureModel]{name: "nonatomic", value: func(data procedureModel) attr.Value { return data.Nonatomic }},
		routineDefinitionAttribute[procedureModel]{name: "configuration", value: func(data procedureModel) attr.Value { return data.Configuration }},
	)
	return append(steps, procedureOwnerStep)
}

// alterProcedureStatements renders the in-place changes from prev to plan, the redefinition before the owner.
func alterProcedureStatements(prev, plan procedureModel) ([]string, error) {
	spec, err := newProcedureSpec(plan)
	if err != nil {
		return nil, err
	}
	return alterStatements(prev, plan, procedureAlterSteps(spec)), nil
}

// dropProcedureStatement renders DROP PROCEDURE with the input types; Redshift documents no CASCADE for it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_PROCEDURE.html
func dropProcedureStatement(data procedureModel) (string, error) {
	signature, err := procedureSignature(procedureArguments(data.Arguments))
	if err != nil {
		return "", err
	}
	return procedureTarget("DROP PROCEDURE", data.Schema.ValueString(), data.Name.ValueString(), signature).String(), nil
}

// readProcedureQuery reads one stored procedure overload from pg_proc_info, where prokind 'p' marks procedures and
// prosecdef SECURITY DEFINER. proargtypes holds only the IN and INOUT types that identify it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_PG_PROC_INFO.html
func readProcedureQuery(schema, name string, signature sqlclient.Keyword) sqlclient.Query {
	return sqlclient.Select("p.proname AS procedure_name", "u.usename AS owner", "p.prosecdef AS security_definer",
		"oidvectortypes(p.proargtypes) AS arguments", "p.prosrc AS body").
		From("pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_user u ON u.usesysid = p.proowner").
		Where("n.nspname = :schema", sqlclient.Bind("schema", schema)).
		Where("p.proname = :name", sqlclient.Bind("name", name)).
		Where("p.prokind = 'p'").
		WhereEither(signature == "", "p.pronargs = 0", "oidvectortypes(p.proargtypes) = :arguments", sqlclient.Bind("arguments", string(signature)))
}

// showProcedureParametersStatement lists every argument with its name, position, and mode, including the OUT
// arguments that pg_proc_info keeps only as type OIDs.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_PARAMETERS.html
func showProcedureParametersStatement(schema, name string, signature sqlclient.Keyword) string {
	return procedureTarget("SHOW PARAMETERS OF PROCEDURE", schema, name, signature).String()
}
