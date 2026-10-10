package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// procedureTestArgument builds one argument; empty name or mode means null.
func procedureTestArgument(name, mode, dataType string) procedureArgumentModel {
	argument := procedureArgumentModel{Name: types.StringNull(), Mode: types.StringNull(), Type: types.StringValue(dataType)}
	if name != "" {
		argument.Name = types.StringValue(name)
	}
	if mode != "" {
		argument.Mode = types.StringValue(mode)
	}
	return argument
}

// procedureTestModel returns a valid procedure model with the given arguments.
func procedureTestModel(arguments ...procedureArgumentModel) procedureModel {
	return procedureModel{
		ID: types.StringNull(), Database: types.StringValue("analytics"), Schema: types.StringValue("public"), Name: types.StringValue("sp_example"),
		Arguments: procedureArgumentList(arguments), Signature: types.StringNull(), Body: types.StringValue("BEGIN total := min_id * 2; END;"),
		DefinitionFingerprint: types.StringNull(), Security: types.StringValue("INVOKER"), Nonatomic: types.BoolNull(),
		Configuration: types.MapNull(types.StringType), Owner: types.StringNull(),
	}
}

// procedureTestDefault returns the lifecycle procedure: one named IN and one named OUT argument.
func procedureTestDefault() procedureModel {
	return procedureTestModel(procedureTestArgument("min_id", "", "integer"), procedureTestArgument("total", "OUT", "bigint"))
}

// procedureTestCreate renders CREATE or CREATE OR REPLACE for a model, recording validation errors.
func procedureTestCreate(data procedureModel, replace bool) func() (string, error) {
	return func() (string, error) {
		spec, err := newProcedureSpec(data)
		if err != nil {
			return "", err
		}
		return createProcedureStatement(spec, replace), nil
	}
}

// procedureTestChange returns the default procedure after change.
func procedureTestChange(change func(*procedureModel)) procedureModel {
	data := procedureTestDefault()
	change(&data)
	return data
}

// procedureTestSettings builds a configuration map.
func procedureTestSettings(settings map[string]string) types.Map {
	values := map[string]attr.Value{}
	for key, value := range settings {
		values[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, values)
}

// TestProcedureSQL pins the procedure statements and catalog reads against the AWS CREATE, ALTER, DROP PROCEDURE and
// SHOW PARAMETERS syntax, including quoting of names, bodies, and SET literals.
func TestProcedureSQL(t *testing.T) {
	quoted := procedureTestModel(procedureTestArgument(`Odd"Arg`, "INOUT", "varchar(20)"), procedureTestArgument("", "", "int"), procedureTestArgument("", "OUT", "refcursor"))
	quoted.Schema, quoted.Name = types.StringValue(`Odd"Schema`), types.StringValue(`SP"Mixed`)
	quoted.Body = types.StringValue(`BEGIN RAISE INFO 'it''s $$ \ %', $1; END;`)
	quoted.Configuration = procedureTestSettings(map[string]string{"search_path": `O'Re\illy`})
	definer := procedureTestChange(func(data *procedureModel) { data.Security = types.StringValue("DEFINER") })
	nonatomic := procedureTestChange(func(data *procedureModel) { data.Nonatomic = types.BoolValue(true) })
	settings := procedureTestChange(func(data *procedureModel) {
		data.Configuration = procedureTestSettings(map[string]string{"datestyle": "ISO, MDY"})
	})
	body := procedureTestChange(func(data *procedureModel) { data.Body = types.StringValue("BEGIN total := min_id; END;") })
	bodyAndSecurity := body
	bodyAndSecurity.Security = types.StringValue("DEFINER")
	owner := procedureTestChange(func(data *procedureModel) { data.Owner = types.StringValue(`Etl"User`) })
	spelled := procedureTestModel(procedureTestArgument("MIN_ID", "IN", "int4"), procedureTestArgument("total", "OUT", "int8"))
	importedLabel := procedureTestModel(procedureTestArgument("min_id", "", "integer"), procedureTestArgument("label", "OUT", "character varying"))
	modifiedLabel := procedureTestModel(procedureTestArgument("min_id", "", "integer"), procedureTestArgument("label", "OUT", "varchar(64)"))
	searchPath := procedureTestChange(func(data *procedureModel) {
		data.Configuration = procedureTestSettings(map[string]string{"search_path": ` $user, Analytics ,O'Re\illy`})
	})
	tooMany := make([]procedureArgumentModel, routineMaxArguments+1)
	for i := range tooMany {
		tooMany[i] = procedureTestArgument("", "INOUT", "integer")
	}
	checkSQL(t, "procedure", []sqlCase{
		{"create", procedureTestCreate(procedureTestDefault(), false)},
		{"create_quoted", procedureTestCreate(quoted, false)},
		{"create_no_arguments", procedureTestCreate(procedureTestModel(), false)},
		{"create_definer", procedureTestCreate(definer, false)},
		{"create_nonatomic", procedureTestCreate(nonatomic, false)},
		{"create_settings", procedureTestCreate(settings, false)},
		{"create_search_path", procedureTestCreate(searchPath, false)},
		{"replace", procedureTestCreate(procedureTestDefault(), true)},
		{"alter_body", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), body) }},
		{"alter_security", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), definer) }},
		{"alter_body_and_security", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), bodyAndSecurity) }},
		{"alter_nonatomic", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), nonatomic) }},
		{"alter_settings", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), settings) }},
		{"alter_owner", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), owner) }},
		{"alter_owner_quoted_signature", func() ([]string, error) {
			after := quoted
			after.Owner = types.StringValue("etl")
			return alterProcedureStatements(quoted, after)
		}},
		{"alter_argument_spelling", func() ([]string, error) { return alterProcedureStatements(procedureTestDefault(), spelled) }},
		{"alter_argument_modifier", func() ([]string, error) { return alterProcedureStatements(importedLabel, modifiedLabel) }},
		{"alter_invalid", func() ([]string, error) {
			return alterProcedureStatements(procedureTestDefault(), procedureTestChange(func(data *procedureModel) { data.Security = types.StringValue("OWNER") }))
		}},
		{"drop", func() (string, error) { return dropProcedureStatement(procedureTestDefault()) }},
		{"drop_quoted", func() (string, error) { return dropProcedureStatement(quoted) }},
		{"drop_no_arguments", func() (string, error) { return dropProcedureStatement(procedureTestModel()) }},
		{"drop_invalid_argument", func() (string, error) {
			return dropProcedureStatement(procedureTestModel(procedureTestArgument("", "", "money")))
		}},
		{"error_nonatomic_definer", procedureTestCreate(procedureTestChange(func(data *procedureModel) {
			data.Nonatomic, data.Security = types.BoolValue(true), types.StringValue("DEFINER")
		}), false)},
		{"error_nonatomic_settings", procedureTestCreate(procedureTestChange(func(data *procedureModel) {
			data.Nonatomic, data.Configuration = types.BoolValue(true), procedureTestSettings(map[string]string{"search_path": "x"})
		}), false)},
		{"error_search_path_empty_schema", procedureTestCreate(procedureTestChange(func(data *procedureModel) {
			data.Configuration = procedureTestSettings(map[string]string{"SEARCH_PATH": "analytics,,public"})
		}), false)},
		{"error_parameter_name", procedureTestCreate(procedureTestChange(func(data *procedureModel) {
			data.Configuration = procedureTestSettings(map[string]string{"search_path TO x; DROP": "x"})
		}), false)},
		{"error_mode", procedureTestCreate(procedureTestModel(procedureTestArgument("a", "VARIADIC", "integer")), false)},
		{"error_anyelement", procedureTestCreate(procedureTestModel(procedureTestArgument("a", "", "anyelement")), false)},
		{"error_unknown_type", procedureTestCreate(procedureTestModel(procedureTestArgument("a", "", "money")), false)},
		{"error_empty_argument_name", procedureTestCreate(procedureTestModel(procedureTestArgument("", "", "integer"), procedureArgumentModel{Name: types.StringValue(""), Mode: types.StringNull(), Type: types.StringValue("integer")}), false)},
		{"error_too_many_inputs", procedureTestCreate(procedureTestModel(tooMany...), false)},
		{"error_empty_body", procedureTestCreate(procedureTestChange(func(data *procedureModel) { data.Body = types.StringValue("") }), false)},
		{"error_empty_schema", procedureTestCreate(procedureTestChange(func(data *procedureModel) { data.Schema = types.StringValue("") }), false)},
		{"read", routineTestQuery(readProcedureQuery(`Odd"Schema`, `SP"Mixed`, "character varying, integer"))},
		{"read_no_arguments", routineTestQuery(readProcedureQuery("public", "sp_example", ""))},
		{"show_parameters", func() string {
			return showProcedureParametersStatement(`Odd"Schema`, `SP"Mixed`, "character varying, integer")
		}},
		{"show_parameters_no_arguments", func() string { return showProcedureParametersStatement("public", "sp_example", "") }},
	})
}

// TestProcedureSignatureUsesInputArguments checks that OUT arguments are not part of the identity, as DROP
// PROCEDURE documents.
func TestProcedureSignatureUsesInputArguments(t *testing.T) {
	signature, err := procedureSignature(procedureArguments(procedureTestModel(
		procedureTestArgument("a", "OUT", "bigint"), procedureTestArgument("b", "INOUT", "varchar(10)"), procedureTestArgument("c", "", "int"),
	).Arguments))
	require.NoError(t, err)
	assert.Equal(t, "character varying, integer", string(signature))
	signature, err = procedureSignature(nil)
	require.NoError(t, err)
	assert.Empty(t, signature)
}

// TestProcedureArgumentsEquivalent separates spelling, and modifiers added to a type stored without them, from
// changes that need a replacement.
func TestProcedureArgumentsEquivalent(t *testing.T) {
	base := []procedureArgumentModel{procedureTestArgument("min_id", "", "integer"), procedureTestArgument("total", "OUT", "bigint")}
	assert.True(t, procedureArgumentsEquivalent(base, []procedureArgumentModel{procedureTestArgument("MIN_ID", "IN", "int"), procedureTestArgument("total", "OUT", "int8")}))
	imported := []procedureArgumentModel{procedureTestArgument("label", "OUT", "character varying")}
	assert.True(t, procedureArgumentsEquivalent(imported, []procedureArgumentModel{procedureTestArgument("label", "OUT", "varchar(64)")}))
	assert.False(t, procedureArgumentsEquivalent([]procedureArgumentModel{procedureTestArgument("label", "OUT", "varchar(32)")},
		[]procedureArgumentModel{procedureTestArgument("label", "OUT", "varchar(64)")}), "a different modifier")
	for name, changed := range map[string][]procedureArgumentModel{
		"type":     {procedureTestArgument("min_id", "", "bigint"), procedureTestArgument("total", "OUT", "bigint")},
		"modifier": {procedureTestArgument("min_id", "", "integer"), procedureTestArgument("total", "OUT", "numeric(10,2)")},
		"mode":     {procedureTestArgument("min_id", "INOUT", "integer"), procedureTestArgument("total", "OUT", "bigint")},
		"name":     {procedureTestArgument("minimum", "", "integer"), procedureTestArgument("total", "OUT", "bigint")},
		"count":    {procedureTestArgument("min_id", "", "integer")},
	} {
		assert.False(t, procedureArgumentsEquivalent(base, changed), name)
	}
}
