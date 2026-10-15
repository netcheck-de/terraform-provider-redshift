package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// templateTestModel builds a template in serving of the admin database with the given format and parameters.
func templateTestModel(name, format, argument string, parameters map[string]string) templateModel {
	model := templateModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue(templateFakeSchema), Name: types.StringValue(name),
		Owner: types.StringNull(), Format: types.StringNull(), FormatArgument: types.StringNull(), Parameters: types.MapNull(types.StringType),
	}
	if format != "" {
		model.Format = types.StringValue(format)
	}
	if argument != "" {
		model.FormatArgument = types.StringValue(argument)
	}
	if parameters != nil {
		elements := map[string]attr.Value{}
		for key, value := range parameters {
			elements[key] = types.StringValue(value)
		}
		model.Parameters = types.MapValueMust(types.StringType, elements)
	}
	return model
}

// templateLifecycleModel is the representative template the fake populates.
var templateLifecycleModel = templateTestModel("csv_load", "CSV", "", map[string]string{"IGNOREHEADER": "1", "DELIMITER": ","})

// TestTemplateSQL pins CREATE, ALTER, DROP, and SHOW TEMPLATE against r_CREATE_TEMPLATE, r_ALTER_TEMPLATE,
// r_DROP_TEMPLATE, r_SHOW_TEMPLATE, and r_SHOW_TEMPLATES.
func TestTemplateSQL(t *testing.T) {
	basic := templateLifecycleModel
	json := templateTestModel("json_load", "json", "auto", map[string]string{"NULL": "", "MAXERROR": "100"})
	every := templateTestModel("every_option", "", "", map[string]string{
		"ACCEPTANYDATE": "", "ACCEPTINVCHARS": "^", "BLANKSASNULL": "", "BZIP2": "", "COMPROWS": "1000000", "COMPUPDATE": "preset",
		"DATEFORMAT": "DD-MM-YYYY", "DELIMITER": "|", "EMPTYASNULL": "", "ENCODING": "utf16", "ENCRYPTED": "", "ESCAPE": "",
		"EXPLICIT_IDS": "", "FILLRECORD": "", "IGNOREALLERRORS": "", "IGNOREBLANKLINES": "", "IGNOREHEADER": "01", "NOLOAD": "",
		"REMOVEQUOTES": "", "ROUNDEC": "", "STATUPDATE": "OFF", "TIMEFORMAT": "epochsecs", "TRIMBLANKS": "", "TRUNCATECOLUMNS": "",
	})
	quoted := templateTestModel(`odd"template`, "FIXEDWIDTH", `id:4,label:20`, map[string]string{"NULL": `it's \N`})
	quoted.Schema = types.StringValue(`Odd"Schema`)
	owned := basic
	owned.Owner = types.StringValue(`Odd"Owner`)
	with := func(model templateModel, change func(*templateModel)) templateModel {
		change(&model)
		return model
	}
	parameters := func(values map[string]string) func(*templateModel) {
		return func(m *templateModel) { m.Parameters = templateTestModel("", "", "", values).Parameters }
	}
	alter := func(prev, plan templateModel) func() ([]string, error) {
		return func() ([]string, error) { return alterTemplateStatements(prev, plan) }
	}
	create := func(model templateModel) func() ([]string, error) {
		return func() ([]string, error) { return createTemplateStatements(model) }
	}
	checkSQL(t, "template", []sqlCase{
		{"create_csv", create(basic)},
		{"create_json", create(json)},
		{"create_every_option", create(every)},
		{"create_quoted", create(quoted)},
		{"create_owner", create(owned)},
		{"alter_unchanged", alter(basic, basic)},
		{"alter_rename", alter(basic, with(basic, func(m *templateModel) { m.Name = types.StringValue("csv_import") }))},
		{"alter_owner", alter(basic, owned)},
		{"alter_add_parameters", alter(basic, with(basic, parameters(map[string]string{"IGNOREHEADER": "1", "DELIMITER": ",", "GZIP": "", "NULL": ""})))},
		{"alter_drop_parameters", alter(basic, with(basic, func(m *templateModel) { m.Parameters = types.MapNull(types.StringType) }))},
		{"alter_set_parameters", alter(basic, with(basic, parameters(map[string]string{"IGNOREHEADER": "2", "DELIMITER": ";"})))},
		{"alter_parameter_spelling", alter(basic, with(basic, parameters(map[string]string{"IGNOREHEADER": "01", "DELIMITER": ","})))},
		{"alter_optional_argument", alter(
			with(basic, parameters(map[string]string{"ACCEPTINVCHARS": ""})),
			with(basic, parameters(map[string]string{"ACCEPTINVCHARS": "?"})))},
		{"alter_format", alter(json, with(json, func(m *templateModel) { m.Format, m.FormatArgument = types.StringValue("CSV"), types.StringNull() }))},
		{"alter_format_case", alter(json, with(json, func(m *templateModel) { m.Format = types.StringValue("JSON") }))},
		{"alter_format_argument", alter(json, with(json, func(m *templateModel) { m.FormatArgument = types.StringValue("auto ignorecase") }))},
		{"alter_format_with_owner", alter(owned, with(owned, func(m *templateModel) { m.Format = types.StringValue("PARQUET") }))},
		{"alter_rename_and_parameters", alter(basic, with(basic, func(m *templateModel) {
			m.Name = types.StringValue("csv_import")
			m.Parameters = templateTestModel("", "", "", map[string]string{"IGNOREHEADER": "1"}).Parameters
		}))},
		{"drop", func() (string, error) { return dropTemplateStatement(basic) }},
		{"drop_quoted", func() (string, error) { return dropTemplateStatement(quoted) }},
		{"show_template", func() string { return showTemplateStatement(quoted) }},
		{"show_templates", func() string { return showTemplatesStatement("", templateFakeSchema, `odd\name_%`) }},
		{"show_templates_qualified", func() string { return showTemplatesStatement(`Odd"Database`, `Odd"Schema`, "") }},
		{"list_schemas", maskingQuerySQL(templateSchemasQuery("analytics"))},
		{"error_unknown_parameter", create(with(basic, parameters(map[string]string{"MANIFEST": ""})))},
		{"error_lowercase_parameter", create(with(basic, parameters(map[string]string{"gzip": ""})))},
		{"error_job_parameter", create(with(basic, parameters(map[string]string{"REGION": "us-east-1"})))},
		{"error_flag_value", create(with(basic, parameters(map[string]string{"GZIP": "yes"})))},
		{"error_integer", create(with(basic, parameters(map[string]string{"IGNOREHEADER": "one"})))},
		{"error_choice", create(with(basic, parameters(map[string]string{"ENCODING": "LATIN1"})))},
		{"error_format", create(with(basic, func(m *templateModel) { m.Format = types.StringValue("XML") }))},
		{"error_format_argument_missing", create(with(json, func(m *templateModel) { m.FormatArgument = types.StringNull() }))},
		{"error_format_argument_unexpected", create(with(basic, func(m *templateModel) { m.FormatArgument = types.StringValue("auto") }))},
		{"error_argument_without_format", create(with(basic, func(m *templateModel) { m.Format, m.FormatArgument = types.StringNull(), types.StringValue("auto") }))},
		{"error_empty", create(templateTestModel("empty", "", "", nil))},
		{"error_uppercase_name", create(with(basic, func(m *templateModel) { m.Name = types.StringValue("CSV_Load") }))},
		{"error_empty_schema", create(with(basic, func(m *templateModel) { m.Schema = types.StringValue("") }))},
		{"error_alter_invalid", alter(basic, with(basic, parameters(map[string]string{"MANIFEST": ""})))},
	})
}

// TestTemplateDefinitionParsing reads the SHOW TEMPLATE outputs the AWS pages print, in any parameter order, with AS
// dropped or kept and FORMAT AS kept.
func TestTemplateDefinitionParsing(t *testing.T) {
	for text, expected := range map[string]copyDefinition{
		// r_SHOW_TEMPLATE
		`CREATE OR REPLACE TEMPLATE dev.public.test_template FOR COPY AS ENCRYPTED NOLOAD ENCODING UTF16 DELIMITER ',';`: {
			parameters: map[string]string{"ENCRYPTED": "", "NOLOAD": "", "ENCODING": "UTF16", "DELIMITER": ","},
		},
		`CREATE OR REPLACE TEMPLATE dev.demo_schema.demo_template FOR COPY AS TRUNCATECOLUMNS NULL 'null_string' EXPLICIT_IDS TIMEFORMAT 'DD.MM.YYYY HH:MI:SS' ACCEPTANYDATE ROUNDEC ACCEPTINVCHARS DATEFORMAT 'DD-MM-YYYY';`: {
			parameters: map[string]string{"TRUNCATECOLUMNS": "", "NULL": "null_string", "EXPLICIT_IDS": "", "TIMEFORMAT": "DD.MM.YYYY HH:MI:SS", "ACCEPTANYDATE": "", "ROUNDEC": "", "ACCEPTINVCHARS": "", "DATEFORMAT": "DD-MM-YYYY"},
		},
		// r_CREATE_TEMPLATE
		`CREATE OR REPLACE TEMPLATE dev.test_schema.demo_template FOR COPY AS FORMAT AS JSON 'auto' NULL '' MAXERROR 100;`: {
			format: "JSON", formatArgument: "auto", parameters: map[string]string{"NULL": "", "MAXERROR": "100"},
		},
		// The statement the provider renders, and spellings the grammar allows.
		`CREATE TEMPLATE "serving"."csv_load" FOR COPY AS FORMAT AS CSV DELIMITER ',' IGNOREHEADER 1`: {
			format: "CSV", parameters: map[string]string{"DELIMITER": ",", "IGNOREHEADER": "1"},
		},
		`create template t for copy csv quote as '"', delimiter as ';', ignoreheader as 2, compupdate off, acceptinvchars '?'`: {
			format: "CSV", parameters: map[string]string{"QUOTE": `"`, "DELIMITER": ";", "IGNOREHEADER": "2", "COMPUPDATE": "OFF", "ACCEPTINVCHARS": "?"},
		},
		`CREATE TEMPLATE t FOR COPY AS FORMAT JSON AS 's3://bucket/paths.json' GZIP`: {
			format: "JSON", formatArgument: "s3://bucket/paths.json", parameters: map[string]string{"GZIP": ""},
		},
	} {
		definition, err := parseTemplateDefinition(text)
		require.NoError(t, err, text)
		assert.Equal(t, expected, definition, text)
	}
	for _, text := range []string{
		`SELECT 1`,
		`CREATE TABLE t (id INTEGER)`,
		`CREATE TEMPLATE t FOR UNLOAD AS PARALLEL OFF`,
		`CREATE TEMPLATE t FOR COPY AS MANIFEST`,
		`CREATE TEMPLATE t FOR COPY AS DELIMITER`,
		`CREATE TEMPLATE t FOR COPY AS IGNOREHEADER 'one'`,
		`CREATE TEMPLATE t FOR COPY AS ENCODING LATIN1`,
		`CREATE TEMPLATE t FOR COPY AS GZIP GZIP`,
		`CREATE TEMPLATE t FOR COPY AS CSV JSON 'auto'`,
		`CREATE TEMPLATE t FOR COPY AS FORMAT AS XML`,
		`CREATE TEMPLATE t FOR COPY AS FORMAT`,
		`CREATE TEMPLATE t FOR COPY AS JSON`,
		`CREATE TEMPLATE t FOR COPY AS DELIMITER 'x' ( GZIP )`,
		`CREATE TEMPLATE t FOR COPY AS NULL 'open`,
	} {
		_, err := parseTemplateDefinition(text)
		require.Error(t, err, text)
	}
}

// TestTemplateDefinitionText reads the statement of a SHOW TEMPLATE row whatever its undocumented column name.
func TestTemplateDefinitionText(t *testing.T) {
	text, err := templateDefinitionText(sqlclient.Row{"anything": "CREATE OR REPLACE TEMPLATE x FOR COPY AS CSV"})
	require.NoError(t, err)
	assert.Equal(t, "CREATE OR REPLACE TEMPLATE x FOR COPY AS CSV", text)
	text, err = templateDefinitionText(sqlclient.Row{"name": "x", "ddl": " create or replace template x for copy as csv"})
	require.NoError(t, err)
	assert.Equal(t, " create or replace template x for copy as csv", text)
	_, err = templateDefinitionText(sqlclient.Row{"name": "x", "id": "1"})
	require.Error(t, err)
}
