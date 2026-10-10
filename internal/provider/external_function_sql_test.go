package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// externalFunctionTestModel is the lifecycle fixture: a one-argument Lambda UDF in schema serving.
func externalFunctionTestModel() externalFunctionModel {
	return externalFunctionModel{
		ID:               types.StringNull(),
		Database:         types.StringValue("admin"),
		Schema:           types.StringValue("serving"),
		Name:             types.StringValue(fakeExternalFunctionName),
		Arguments:        externalFunctionTypeValues([]string{"varchar"}),
		ReturnType:       types.StringValue("varchar"),
		Volatility:       types.StringValue("STABLE"),
		LambdaFunction:   types.StringValue("exfunc_upper"),
		IAMRole:          types.StringValue("arn:aws:iam::123456789012:role/lambda-udf"),
		RetryTimeout:     types.Int64Null(),
		MaxBatchRows:     types.Int64Null(),
		MaxBatchSize:     types.Int64Null(),
		MaxBatchSizeUnit: types.StringNull(),
		Owner:            types.StringValue("admin"),
	}
}

// externalFunctionWith returns a copy of the fixture with change applied.
func externalFunctionWith(change func(*externalFunctionModel)) externalFunctionModel {
	data := externalFunctionTestModel()
	change(&data)
	return data
}

// TestExternalFunctionSQL pins every external function statement and catalog read against the CREATE EXTERNAL
// FUNCTION, ALTER FUNCTION, and DROP FUNCTION references, including quoting and rejected options.
func TestExternalFunctionSQL(t *testing.T) {
	create := func(data externalFunctionModel) func() ([]string, error) {
		return func() ([]string, error) { return createExternalFunctionStatements(data) }
	}
	alter := func(prev, plan externalFunctionModel) func() ([]string, error) {
		return func() ([]string, error) { return alterExternalFunctionStatements(prev, plan) }
	}
	drop := func(data externalFunctionModel) func() (string, error) {
		return func() (string, error) { return dropExternalFunctionStatement(data) }
	}
	read := func(data externalFunctionModel) func() (string, error) {
		return func() (string, error) {
			query, err := readExternalFunctionQuery(data)
			if err != nil {
				return "", err
			}
			sql, _, err := query.Build()
			return sql, err
		}
	}
	minimal := externalFunctionWith(func(data *externalFunctionModel) {
		data.Arguments, data.ReturnType, data.Volatility = externalFunctionTypeValues([]string{}), types.StringValue("int"), types.StringNull()
		data.IAMRole, data.Owner = types.StringValue("default"), types.StringUnknown()
	})
	full := externalFunctionWith(func(data *externalFunctionModel) {
		data.Arguments = externalFunctionTypeValues([]string{"int", "VARCHAR(10)", "decimal(10,2)", "timestamp", "bool"})
		data.RetryTimeout, data.MaxBatchRows, data.MaxBatchSize = types.Int64Value(0), types.Int64Value(100), types.Int64Value(512)
	})
	quoted := externalFunctionWith(func(data *externalFunctionModel) {
		data.Schema, data.Name, data.Owner = types.StringValue(`Odd"Schema`), types.StringValue(`F"Upper`), types.StringValue(`Odd"Owner`)
		data.LambdaFunction = types.StringValue(`it's \lambda`)
	})
	invalid := func(change func(*externalFunctionModel)) func() ([]string, error) {
		return create(externalFunctionWith(change))
	}
	imported := externalFunctionWith(func(data *externalFunctionModel) {
		data.LambdaFunction, data.IAMRole, data.ReturnType = types.StringNull(), types.StringNull(), types.StringValue("character varying")
	})
	checkSQL(t, "external_function", []sqlCase{
		{"create", create(externalFunctionTestModel())},
		{"create_minimal", create(minimal)},
		{"create_all_options", create(full)},
		{"create_quoted", create(quoted)},
		{"create_chained_roles", create(externalFunctionWith(func(data *externalFunctionModel) {
			data.IAMRole = types.StringValue("arn:aws:iam::123456789012:role/redshift,arn:aws:iam::210987654321:role/lambda")
		}))},
		{"create_batch_size_mb", create(externalFunctionWith(func(data *externalFunctionModel) {
			data.MaxBatchSize, data.MaxBatchSizeUnit = types.Int64Value(5), types.StringValue("MB")
		}))},
		{"create_batch_size_kb", create(externalFunctionWith(func(data *externalFunctionModel) {
			data.MaxBatchSize, data.MaxBatchSizeUnit = types.Int64Value(5120), types.StringValue("KB")
		}))},
		{"create_error_unsupported_argument", invalid(func(data *externalFunctionModel) {
			data.Arguments = externalFunctionTypeValues([]string{"int", "super"})
		})},
		{"create_error_unknown_argument", invalid(func(data *externalFunctionModel) {
			data.Arguments = externalFunctionTypeValues([]string{"int); DROP TABLE t; --"})
		})},
		{"create_error_too_many_arguments", invalid(func(data *externalFunctionModel) {
			data.Arguments = externalFunctionTypeValues(strings.Split(strings.Repeat("int,", 33)[:4*33-1], ","))
		})},
		{"create_error_unsupported_return", invalid(func(data *externalFunctionModel) { data.ReturnType = types.StringValue("geometry") })},
		{"create_error_immutable", invalid(func(data *externalFunctionModel) { data.Volatility = types.StringValue("IMMUTABLE") })},
		{"create_error_empty_lambda", invalid(func(data *externalFunctionModel) { data.LambdaFunction = types.StringValue("") })},
		{"create_error_iam_role", invalid(func(data *externalFunctionModel) {
			data.IAMRole = types.StringValue("arn:aws:iam::123456789012:role/a' IAM_ROLE 'b")
		})},
		{"create_error_negative_retry", invalid(func(data *externalFunctionModel) { data.RetryTimeout = types.Int64Value(-1) })},
		{"create_error_batch_rows", invalid(func(data *externalFunctionModel) { data.MaxBatchRows = types.Int64Value(0) })},
		{"create_error_batch_size", invalid(func(data *externalFunctionModel) {
			data.MaxBatchSize, data.MaxBatchSizeUnit = types.Int64Value(6), types.StringValue("MB")
		})},
		{"create_error_batch_unit_alone", invalid(func(data *externalFunctionModel) { data.MaxBatchSizeUnit = types.StringValue("MB") })},
		{"create_error_batch_unit", invalid(func(data *externalFunctionModel) {
			data.MaxBatchSize, data.MaxBatchSizeUnit = types.Int64Value(1), types.StringValue("GB")
		})},
		{"alter_unchanged", alter(externalFunctionTestModel(), externalFunctionTestModel())},
		{"alter_type_spelling", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) {
			data.Arguments, data.ReturnType = externalFunctionTypeValues([]string{"character varying"}), types.StringValue("text")
		}))},
		{"alter_return_length", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) {
			data.ReturnType = types.StringValue("varchar(512)")
		}))},
		{"alter_lambda", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) {
			data.LambdaFunction = types.StringValue("arn:aws:lambda:eu-central-1:123456789012:function:exfunc_upper_v2")
		}))},
		{"alter_several_options", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) {
			data.Volatility, data.IAMRole, data.RetryTimeout = types.StringValue("VOLATILE"), types.StringValue("default"), types.Int64Value(3000)
			data.MaxBatchRows, data.MaxBatchSize, data.MaxBatchSizeUnit = types.Int64Value(50), types.Int64Value(1), types.StringValue("MB")
		}))},
		{"alter_reset_options", alter(full, externalFunctionWith(func(data *externalFunctionModel) {
			data.Arguments = full.Arguments
		}))},
		{"alter_owner", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) { data.Owner = types.StringValue(`Odd"Owner`) }))},
		{"alter_owner_unknown", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) { data.Owner = types.StringUnknown() }))},
		{"alter_definition_and_owner", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) {
			data.Volatility, data.Owner = types.StringValue("VOLATILE"), types.StringValue("etl")
		}))},
		{"alter_after_import", alter(imported, externalFunctionTestModel())},
		{"alter_quoted", alter(externalFunctionWith(func(data *externalFunctionModel) {
			data.Schema, data.Name = quoted.Schema, quoted.Name
		}), quoted)},
		{"alter_error_invalid_plan", alter(externalFunctionTestModel(), externalFunctionWith(func(data *externalFunctionModel) { data.IAMRole = types.StringValue("role") }))},
		{"drop", drop(externalFunctionTestModel())},
		{"drop_quoted", drop(quoted)},
		{"drop_without_arguments", drop(minimal)},
		{"drop_canonical_signature", drop(full)},
		{"read", read(externalFunctionTestModel())},
		{"read_without_arguments", read(minimal)},
		{"read_error_invalid_argument", read(externalFunctionWith(func(data *externalFunctionModel) {
			data.Arguments = externalFunctionTypeValues([]string{"hstore"})
		}))},
	})
}

// TestExternalFunctionReadBindings sends the canonical signature and names as bindings, never as SQL text.
func TestExternalFunctionReadBindings(t *testing.T) {
	data := externalFunctionWith(func(data *externalFunctionModel) {
		data.Schema, data.Name = types.StringValue(`Odd"Schema`), types.StringValue(`it's \f`)
		data.Arguments = externalFunctionTypeValues([]string{"int4", "varchar(10)"})
	})
	query, err := readExternalFunctionQuery(data)
	require.NoError(t, err)
	_, parameters, err := query.Build()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"schema": `Odd"Schema`, "name": `it's \f`, "arguments": "integer, character varying"}, parameters)
	query, err = readExternalFunctionQuery(externalFunctionWith(func(data *externalFunctionModel) {
		data.Arguments = externalFunctionTypeValues(nil)
	}))
	require.NoError(t, err)
	_, parameters, err = query.Build()
	require.NoError(t, err)
	assert.NotContains(t, parameters, "arguments", "a function without arguments must not bind an empty signature")
}

// TestExternalFunctionAlterCoverage keeps an update step for every in-place attribute.
func TestExternalFunctionAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newExternalFunctionResource(), externalFunctionAlterSteps)
}

// TestExternalFunctionTypes checks the Lambda UDF type allowlist and the canonical signature.
func TestExternalFunctionTypes(t *testing.T) {
	for _, accepted := range []string{"int2", "INTEGER", "int8", "decimal(10,2)", "float4", "float8", "char(3)", "nvarchar(20)", "text", "bool", "date", "timestamp"} {
		_, err := externalFunctionType(accepted)
		require.NoError(t, err, accepted)
	}
	for _, rejected := range []string{"super", "geometry", "timestamptz", "varbyte", "time", "interval year to month", "unknown"} {
		_, err := externalFunctionType(rejected)
		require.Error(t, err, rejected)
	}
	signature, err := externalFunctionSignature([]string{"int", "varchar(10)", "decimal(10,2)"})
	require.NoError(t, err)
	assert.Equal(t, "INTEGER, CHARACTER VARYING, NUMERIC", string(signature))
	assert.Equal(t, []string{"INTEGER", "CHARACTER VARYING", "NUMERIC"}, externalFunctionSignatureTypes(string(signature)))
	assert.Empty(t, externalFunctionSignatureTypes(""))
	for code, keyword := range map[string]string{"v": "VOLATILE", "s": "STABLE", "i": "IMMUTABLE"} {
		volatility, err := externalFunctionVolatility(code)
		require.NoError(t, err)
		assert.Equal(t, keyword, volatility)
	}
	_, err = externalFunctionVolatility("x")
	assert.Error(t, err)
}
