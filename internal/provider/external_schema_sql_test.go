package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestExternalSchemaSQL pins every external schema statement, including quoting of names and catalog options.
func TestExternalSchemaSQL(t *testing.T) {
	model := func(name, glue string, region types.String) externalSchemaModel {
		return externalSchemaModel{
			Database: types.StringValue("warehouse"), Name: types.StringValue(name), GlueDatabase: types.StringValue(glue),
			IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), Region: region,
		}
	}
	create := func(data externalSchemaModel) func() (string, error) {
		return func() (string, error) { return createExternalSchemaStatement(data) }
	}
	plain := model("example_external", "example_glue", types.StringNull())
	quoted := model(`Lake"Schema`, `it's \glue`, types.StringValue(`eu-'central\1`))
	checkSQL(t, "external_schema", []sqlCase{
		{"create", create(plain)},
		{"create_region", create(model("example_external", "example_glue", types.StringValue("eu-central-1")))},
		{"create_unknown_region", create(model("example_external", "example_glue", types.StringUnknown()))},
		{"create_quoted", create(quoted)},
		{"drop", func() string { return dropExternalSchemaStatement(plain) }},
		{"drop_quoted", func() string { return dropExternalSchemaStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readExternalSchemaQuery(quoted).Build()
			assert.Equal(t, map[string]string{"name": `Lake"Schema`}, parameters)
			return sql, err
		}},
	})
}
