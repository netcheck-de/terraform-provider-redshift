package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// functionTestModel returns a valid function model with the given input types.
func functionTestModel(arguments ...string) functionModel {
	values := make([]attr.Value, len(arguments))
	for i, argument := range arguments {
		values[i] = types.StringValue(argument)
	}
	list := types.ListNull(types.StringType)
	if len(arguments) > 0 {
		list = types.ListValueMust(types.StringType, values)
	}
	return functionModel{
		ID: types.StringNull(), Database: types.StringValue("analytics"), Schema: types.StringValue("public"), Name: types.StringValue("f_example"),
		Arguments: list, Signature: types.StringNull(), ReturnType: types.StringValue("integer"), Volatility: types.StringValue("IMMUTABLE"),
		Language: types.StringValue("sql"), Body: types.StringValue("SELECT $1 + 1"), DefinitionFingerprint: types.StringNull(), Owner: types.StringNull(),
	}
}

// functionTestCreate renders CREATE or CREATE OR REPLACE for a model, recording validation errors.
func functionTestCreate(data functionModel, replace bool) func() (string, error) {
	return func() (string, error) {
		spec, err := newFunctionSpec(data)
		if err != nil {
			return "", err
		}
		return createFunctionStatement(spec, replace), nil
	}
}

// routineTestQuery renders a catalog query for a golden case.
func routineTestQuery(query sqlclient.Query) func() (string, error) {
	return func() (string, error) {
		sql, _, err := query.Build()
		return sql, err
	}
}

// TestFunctionSQL pins the function statements and catalog read against the AWS CREATE, ALTER, and DROP FUNCTION
// syntax, including names, bodies, and types that need quoting or canonicalization.
func TestFunctionSQL(t *testing.T) {
	quoted := functionTestModel("int", "VARCHAR(10)", "numeric(12)")
	quoted.Schema, quoted.Name = types.StringValue(`Odd"Schema`), types.StringValue(`F"Mixed`)
	quoted.Body = types.StringValue(`SELECT CASE WHEN $2 = 'it''s $$' THEN $1 ELSE 0 END -- \ path`)
	quoted.ReturnType, quoted.Volatility = types.StringValue("float"), types.StringValue("stable")
	body := functionTestModel("integer")
	body.Body = types.StringValue("SELECT $1 * 2")
	volatility := functionTestModel("integer")
	volatility.Volatility = types.StringValue("STABLE")
	both := body
	both.Volatility = types.StringValue("VOLATILE")
	owner := functionTestModel("integer")
	owner.Owner = types.StringValue(`Etl"User`)
	ownerBefore := functionTestModel("integer")
	ownerBefore.Owner = types.StringValue("admin")
	spelled := functionTestModel("int4")
	imported := functionTestModel("integer", "character varying")
	imported.ReturnType = types.StringValue("character varying")
	modified := functionTestModel("int", "varchar(64)")
	modified.ReturnType = types.StringValue("varchar(128)")
	invalid := func(change func(*functionModel)) functionModel {
		data := functionTestModel("integer")
		change(&data)
		return data
	}
	tooMany := make([]string, routineMaxArguments+1)
	for i := range tooMany {
		tooMany[i] = "integer"
	}
	checkSQL(t, "function", []sqlCase{
		{"create", functionTestCreate(functionTestModel("integer", "varchar(10)"), false)},
		{"create_quoted", functionTestCreate(quoted, false)},
		{"create_no_arguments", functionTestCreate(functionTestModel(), false)},
		{"create_default_volatility", functionTestCreate(invalid(func(data *functionModel) { data.Volatility, data.Language = types.StringNull(), types.StringNull() }), false)},
		{"replace", functionTestCreate(functionTestModel("integer"), true)},
		{"alter_body", func() ([]string, error) { return alterFunctionStatements(functionTestModel("integer"), body) }},
		{"alter_volatility", func() ([]string, error) { return alterFunctionStatements(functionTestModel("integer"), volatility) }},
		{"alter_body_and_volatility", func() ([]string, error) { return alterFunctionStatements(functionTestModel("integer"), both) }},
		{"alter_owner", func() ([]string, error) { return alterFunctionStatements(ownerBefore, owner) }},
		{"alter_owner_quoted_signature", func() ([]string, error) {
			after := quoted
			after.Owner = types.StringValue("etl")
			return alterFunctionStatements(quoted, after)
		}},
		{"alter_unchanged", func() ([]string, error) {
			return alterFunctionStatements(functionTestModel("integer"), functionTestModel("integer"))
		}},
		{"alter_argument_spelling", func() ([]string, error) { return alterFunctionStatements(functionTestModel("integer"), spelled) }},
		{"alter_type_modifiers", func() ([]string, error) { return alterFunctionStatements(imported, modified) }},
		{"alter_invalid", func() ([]string, error) {
			return alterFunctionStatements(functionTestModel("integer"), invalid(func(data *functionModel) { data.Body = types.StringValue(" ") }))
		}},
		{"drop", func() (string, error) { return dropFunctionStatement(functionTestModel("int", "varchar(10)")) }},
		{"drop_quoted", func() (string, error) { return dropFunctionStatement(quoted) }},
		{"drop_no_arguments", func() (string, error) { return dropFunctionStatement(functionTestModel()) }},
		{"drop_invalid_argument", func() (string, error) { return dropFunctionStatement(functionTestModel("money")) }},
		{"error_python", functionTestCreate(invalid(func(data *functionModel) { data.Language = types.StringValue("plpythonu") }), false)},
		{"error_language", functionTestCreate(invalid(func(data *functionModel) { data.Language = types.StringValue("plpgsql") }), false)},
		{"error_language_case", functionTestCreate(invalid(func(data *functionModel) { data.Language = types.StringValue("SQL") }), false)},
		{"error_anyelement", functionTestCreate(functionTestModel("anyelement"), false)},
		{"error_refcursor_return", functionTestCreate(invalid(func(data *functionModel) { data.ReturnType = types.StringValue("refcursor") }), false)},
		{"error_unknown_type", functionTestCreate(functionTestModel("integer", "money"), false)},
		{"error_too_many_arguments", functionTestCreate(functionTestModel(tooMany...), false)},
		{"error_volatility", functionTestCreate(invalid(func(data *functionModel) { data.Volatility = types.StringValue("LEAKPROOF") }), false)},
		{"error_empty_body", functionTestCreate(invalid(func(data *functionModel) { data.Body = types.StringValue("  ") }), false)},
		{"error_empty_name", functionTestCreate(invalid(func(data *functionModel) { data.Name = types.StringValue("") }), false)},
		{"read", routineTestQuery(readFunctionQuery(`Odd"Schema`, `F"Mixed`, "integer, character varying"))},
		{"read_no_arguments", routineTestQuery(readFunctionQuery("public", "f_example", ""))},
	})
}

// TestFunctionSignatureIgnoresModifiers checks that the overload identity drops modifiers and aliases, as
// oidvectortypes reports it.
func TestFunctionSignatureIgnoresModifiers(t *testing.T) {
	for expected, arguments := range map[string][]string{
		"integer, character varying, numeric, character": {"int4", "varchar(10)", "decimal(12,2)", "bpchar"},
		"": nil,
		"interval day to second, double precision": {"INTERVAL DAY TO SECOND(3)", "float"},
	} {
		signature, err := functionSignature(functionTestModel(arguments...))
		require.NoError(t, err)
		assert.Equal(t, expected, string(signature))
	}
}
