package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExternalModelModel returns the representative model of the fake catalog.
func testExternalModelModel() externalModelModel {
	return externalModelModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue(fakeModelSchema), Name: types.StringValue(fakeModelName),
		Function: types.StringValue(fakeModelFunction), IAMRole: types.StringValue("DEFAULT"), ModelID: types.StringValue(fakeModelID),
		Prompt: types.StringValue(fakeModelPrompt), Suffix: types.StringNull(), RequestType: types.StringValue("UNIFIED"), ResponseType: types.StringValue("VARCHAR"),
		Owner: types.StringValue(fakeModelOwner),
	}
}

// TestExternalModelSQL pins every model statement, including quoting, the RAW and UNIFIED forms, and the rejected
// combinations.
func TestExternalModelSQL(t *testing.T) {
	with := func(change func(*externalModelModel)) externalModelModel {
		data := testExternalModelModel()
		change(&data)

		return data
	}
	create := func(data externalModelModel) func() (string, error) {
		return func() (string, error) { return createExternalModelStatement(data) }
	}
	raw := with(func(data *externalModelModel) {
		data.RequestType, data.ResponseType, data.Prompt = types.StringValue("RAW"), types.StringValue("SUPER"), types.StringNull()
		data.IAMRole = types.StringValue("arn:aws:iam::123456789012:role/redshift-bedrock")
	})
	quoted := with(func(data *externalModelModel) {
		data.Schema, data.Name, data.Function = types.StringValue(`Odd"Schema`), types.StringValue(`Odd"Model`), types.StringValue(`Odd"Function`)
		data.Prompt, data.Suffix = types.StringValue(`Answer as 'Bob' \ briefly:`), types.StringValue("Thanks.")
	})
	checkSQL(t, "external_model", []sqlCase{
		{"create_unified", create(testExternalModelModel())},
		{"create_unified_defaults", create(with(func(data *externalModelModel) {
			data.Prompt, data.RequestType, data.ResponseType, data.IAMRole = types.StringNull(), types.StringNull(), types.StringNull(), types.StringValue("default")
		}))},
		{"create_unified_super", create(with(func(data *externalModelModel) { data.ResponseType = types.StringValue("super") }))},
		{"create_raw", create(raw)},
		{"create_quoted", create(quoted)},
		{"create_raw_varchar", create(with(func(data *externalModelModel) {
			data.RequestType, data.Prompt = types.StringValue("RAW"), types.StringNull()
		}))},
		{"create_raw_prompt", create(with(func(data *externalModelModel) {
			data.RequestType, data.ResponseType = types.StringValue("RAW"), types.StringValue("SUPER")
		}))},
		{"create_raw_suffix", create(with(func(data *externalModelModel) {
			data.RequestType, data.ResponseType, data.Prompt, data.Suffix = types.StringValue("RAW"), types.StringValue("SUPER"), types.StringNull(), types.StringValue("x")
		}))},
		{"create_unknown_request_type", create(with(func(data *externalModelModel) { data.RequestType = types.StringValue("STREAM") }))},
		{"create_unknown_response_type", create(with(func(data *externalModelModel) { data.ResponseType = types.StringValue("JSON") }))},
		{"create_chained_roles", create(with(func(data *externalModelModel) {
			data.IAMRole = types.StringValue("arn:aws:iam::123456789012:role/a,arn:aws:iam::123456789012:role/b")
		}))},
		{"create_role_injection", create(with(func(data *externalModelModel) { data.IAMRole = types.StringValue("x' MODEL_TYPE BEDROCK --") }))},
		{"create_empty_function", create(with(func(data *externalModelModel) { data.Function = types.StringValue("") }))},
		{"create_empty_model_id", create(with(func(data *externalModelModel) { data.ModelID = types.StringValue("") }))},
		{"create_empty_schema", create(with(func(data *externalModelModel) { data.Schema = types.StringValue("") }))},
		{"drop", func() (string, error) { return dropExternalModelStatement(testExternalModelModel()) }},
		{"drop_quoted", func() (string, error) { return dropExternalModelStatement(quoted) }},
		{"drop_empty_name", func() (string, error) {
			return dropExternalModelStatement(with(func(data *externalModelModel) { data.Name = types.StringValue("") }))
		}},
		{"list", listModelsStatement},
		{"read", func() (string, error) { return readExternalModelStatement(fakeModelSchema, fakeModelName) }},
		{"read_quoted", func() (string, error) { return readExternalModelStatement(`Odd"Schema`, `Odd"Model`) }},
	})
}

// TestShowModelParsing decodes the documented SHOW MODEL layouts, whatever spelling the transport reports for the
// column labels.
func TestShowModelParsing(t *testing.T) {
	models, err := parseShowModels([]sqlclient.Row{
		{"Schema Name": "serving", "Model Name": "zeta"},
		{"schema_name": " public ", "model_name": "customer_churn"},
		{"SCHEMA NAME": "serving", "MODEL NAME": "alpha"},
	})
	require.NoError(t, err)
	assert.Equal(t, []showModelEntry{{"public", "customer_churn"}, {"serving", "alpha"}, {"serving", "zeta"}}, models)

	_, err = parseShowModels([]sqlclient.Row{{"Schema Name": "serving"}})
	require.Error(t, err)

	settings := parseShowModel([]sqlclient.Row{
		{"Key": "Model Name", "Value": "customer_churn"},
		{"Key": "Owner", "Value": "'owner'"},
		{"Key": "", "Value": ""},
		{"Key": "TRAINING DATA:", "Value": ""},
		{"key": "Function Name", "value": " predict_churn "},
		{"Key": "IAM Role", "Value": "'iam_role'"},
		{"Key": "validation:F1", "Value": "0.855"},
		{"Key": "Owner", "Value": "'later'"},
	})
	assert.Equal(t, map[string]string{
		"model_name": "customer_churn", "owner": "owner", "function_name": "predict_churn", "iam_role": "iam_role", "validation:f1": "0.855",
	}, settings)
	assert.Equal(t, "'", showModelValue("'"), "a lone quote is not a quoted value")
}

// TestObserveExternalModel keeps configured spellings that name the reported values, adopts other reported values,
// and keeps every setting SHOW MODEL does not report.
func TestObserveExternalModel(t *testing.T) {
	data := testExternalModelModel()
	data.RequestType = types.StringValue("unified")
	observeExternalModel(&data, map[string]string{
		"owner": "admin", "function_name": `serving.f_summarize`, "iam_role": "default", "model_id": fakeModelID, "request_type": "UNIFIED",
	})

	expected := testExternalModelModel()
	expected.RequestType, expected.Owner = types.StringValue("unified"), types.StringValue("admin")
	assert.Equal(t, expected, data)

	// SHOW MODEL values are trimmed, so surrounding whitespace in a configured value is kept rather than drift.
	padded := testExternalModelModel()
	padded.Prompt, padded.Suffix = types.StringValue("Summarize the following text:\n"), types.StringValue(" Answer: ")
	observeExternalModel(&padded, map[string]string{"prompt": "Summarize the following text:", "suffix": "Answer:"})
	assert.Equal(t, types.StringValue("Summarize the following text:\n"), padded.Prompt)
	assert.Equal(t, types.StringValue(" Answer: "), padded.Suffix)

	observeExternalModel(&data, map[string]string{
		"function_name": "f_other", "iam_role": "arn:aws:iam::123456789012:role/other", "model_id": "amazon.nova-pro-v1:0",
		"prompt": "Other:", "suffix": "Done.", "request_type": "raw", "response_type": "super",
	})
	assert.Equal(t, types.StringValue("f_other"), data.Function)
	assert.Equal(t, types.StringValue("DEFAULT"), data.IAMRole, "a configured DEFAULT names whichever role the catalog shows")
	assert.Equal(t, types.StringValue("amazon.nova-pro-v1:0"), data.ModelID)
	assert.Equal(t, types.StringValue("Other:"), data.Prompt)
	assert.Equal(t, types.StringValue("Done."), data.Suffix)
	assert.Equal(t, types.StringValue("RAW"), data.RequestType)
	assert.Equal(t, types.StringValue("SUPER"), data.ResponseType)

	imported := externalModelModel{Owner: types.StringUnknown(), IAMRole: types.StringNull()}
	observeExternalModel(&imported, map[string]string{"iam_role": "arn:aws:iam::123456789012:role/other"})
	assert.Equal(t, types.StringValue("arn:aws:iam::123456789012:role/other"), imported.IAMRole)
	assert.True(t, imported.Owner.IsNull(), "an unreported owner is null rather than unknown")
	assert.True(t, imported.ModelID.IsNull(), "an unreported setting keeps its state")
}
