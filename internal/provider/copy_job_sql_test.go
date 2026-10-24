package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyJobTableObject builds a table block value.
func copyJobTableObject(schema, name string, columns ...string) types.Object {
	list := types.ListNull(types.StringType)

	if len(columns) > 0 {
		elements := make([]attr.Value, len(columns))
		for i, column := range columns {
			elements[i] = types.StringValue(column)
		}

		list = types.ListValueMust(types.StringType, elements)
	}

	return types.ObjectValueMust(copyJobTableType.AttrTypes, map[string]attr.Value{"schema": types.StringValue(schema), "name": types.StringValue(name), "columns": list})
}

// copyJobTestModel builds a CSV job in the admin database loading serving.events from an S3 prefix.
func copyJobTestModel(name string, parameters map[string]string) copyJobModel {
	model := copyJobModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Name: types.StringValue(name),
		Table: copyJobTableObject("serving", "events", "id", "label"), Source: types.StringValue("s3://example-bucket/events/"),
		IAMRole: types.StringValue(copyJobFakeRole), Format: types.StringValue("CSV"), FormatArgument: types.StringNull(),
		Parameters: types.MapNull(types.StringType), Auto: types.BoolValue(false),
	}
	if parameters != nil {
		model.Parameters = templateTestModel("", "", "", parameters).Parameters
	}

	return model
}

// copyJobLifecycleModel is the representative job the fake populates.
var copyJobLifecycleModel = copyJobTestModel("events_load", map[string]string{"IGNOREHEADER": "1"})

// TestCopyJobSQL pins COPY JOB CREATE, ALTER, and DROP and the SYS_COPY_JOB reads against r_COPY-JOB and
// SYS_COPY_JOB.
func TestCopyJobSQL(t *testing.T) {
	basic := copyJobLifecycleModel
	with := func(change func(*copyJobModel)) copyJobModel {
		model := basic
		change(&model)

		return model
	}
	create := func(model copyJobModel) func() (string, error) {
		return func() (string, error) { return createCopyJobStatement(model) }
	}
	automatic := with(func(m *copyJobModel) { m.Auto = types.BoolValue(true) })
	checkSQL(t, "copy_job", []sqlCase{
		{"create", create(basic)},
		{"create_auto_default", create(with(func(m *copyJobModel) { m.Auto = types.BoolUnknown() }))},
		{"create_json_all_columns", create(with(func(m *copyJobModel) {
			m.Table, m.Format, m.FormatArgument = copyJobTableObject("serving", "events"), types.StringValue("json"), types.StringValue("auto ignorecase")
			m.Parameters = templateTestModel("", "", "", map[string]string{"GZIP": "", "REGION": "eu-central-1", "TIMEFORMAT": "auto"}).Parameters
			m.Auto = types.BoolValue(true)
		}))},
		{"create_default_format", create(with(func(m *copyJobModel) { m.Format, m.Parameters = types.StringNull(), types.MapNull(types.StringType) }))},
		{"create_quoted", create(with(func(m *copyJobModel) {
			m.Table = copyJobTableObject(`Odd"Schema`, `Odd"Table`, `Odd"Column`)
			m.Source = types.StringValue(`s3://example-bucket/it's/`)
			m.Parameters = templateTestModel("", "", "", map[string]string{"NULL": `\N`}).Parameters
		}))},
		{"alter_auto_on", func() ([]string, error) { return alterCopyJobStatements(basic, automatic) }},
		{"alter_auto_off", func() ([]string, error) { return alterCopyJobStatements(automatic, basic) }},
		{"alter_unchanged", func() ([]string, error) { return alterCopyJobStatements(basic, basic) }},
		{"drop", func() (string, error) { return dropCopyJobStatement(basic) }},
		{"read", maskingQuerySQL(readCopyJobQuery("events_load"))},
		{"list", maskingQuerySQL(listCopyJobsQuery())},
		{"error_hyphen", create(with(func(m *copyJobModel) { m.Name = types.StringValue("events-load") }))},
		{"error_uppercase", create(with(func(m *copyJobModel) { m.Name = types.StringValue("Events") }))},
		{"error_injection", create(with(func(m *copyJobModel) { m.Name = types.StringValue("x; DROP TABLE t") }))},
		{"error_maxerror", create(with(func(m *copyJobModel) {
			m.Parameters = templateTestModel("", "", "", map[string]string{"MAXERROR": "10"}).Parameters
		}))},
		{"error_ignoreallerrors", create(with(func(m *copyJobModel) {
			m.Parameters = templateTestModel("", "", "", map[string]string{"IGNOREALLERRORS": ""}).Parameters
		}))},
		{"error_manifest", create(with(func(m *copyJobModel) {
			m.Parameters = templateTestModel("", "", "", map[string]string{"MANIFEST": ""}).Parameters
		}))},
		{"error_source", create(with(func(m *copyJobModel) { m.Source = types.StringValue("https://bucket/prefix") }))},
		{"error_default_role", create(with(func(m *copyJobModel) { m.IAMRole = types.StringValue("DEFAULT") }))},
		{"error_table", create(with(func(m *copyJobModel) { m.Table = types.ObjectNull(copyJobTableType.AttrTypes) }))},
		{"error_empty_column", create(with(func(m *copyJobModel) { m.Table = copyJobTableObject("serving", "events", "") }))},
		{"error_format_argument", create(with(func(m *copyJobModel) { m.Format = types.StringValue("JSON") }))},
		{"error_drop_name", func() (string, error) {
			return dropCopyJobStatement(with(func(m *copyJobModel) { m.Name = types.StringValue("bad-name") }))
		}},
		{"error_alter_name", func() ([]string, error) {
			return alterCopyJobStatements(basic, with(func(m *copyJobModel) { m.Name = types.StringValue("bad-name") }))
		}},
	})
}

// TestCopyJobCatalogValues reads is_auto in its documented and boolean spellings and keeps a configured role the
// catalog column cut.
func TestCopyJobCatalogValues(t *testing.T) {
	for text, expected := range map[string]bool{"1": true, "0": false, "t": true, "false": false} {
		value, err := copyJobAutoValue(text)
		require.NoError(t, err, text)
		assert.Equal(t, expected, value, text)
	}

	_, err := copyJobAutoValue("2")
	require.Error(t, err)

	long := types.StringValue("arn:aws:iam::123456789012:role/" + strings.Repeat("r", 120))
	assert.Equal(t, long, copyJobRoleValue(long, long.ValueString()[:copyJobRoleLength]+"  "))
	assert.Equal(t, types.StringValue(copyJobFakeRole), copyJobRoleValue(types.StringNull(), copyJobFakeRole+"   "))

	other := "arn:aws:iam::123456789012:role/other"
	assert.Equal(t, types.StringValue(other), copyJobRoleValue(types.StringValue(copyJobFakeRole), other))
}
