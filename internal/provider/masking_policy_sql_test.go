package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maskingTestPolicy builds a policy model in the admin database; without columns it uses one VARCHAR(256) email.
func maskingTestPolicy(name, expression string, columns ...maskingPolicyColumn) maskingPolicyModel {
	if len(columns) == 0 {
		columns = []maskingPolicyColumn{{Name: "email", Type: "VARCHAR(256)"}}
	}
	return maskingPolicyModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Name: types.StringValue(name),
		InputColumn: maskingPolicyColumnList(columns), Expression: types.StringValue(expression), DefinitionFingerprint: types.StringNull(),
	}
}

// maskingQuerySQL renders a catalog query for golden files.
func maskingQuerySQL(query sqlclient.Query) func() (string, error) {
	return func() (string, error) {
		sql, _, err := query.Build()
		return sql, err
	}
}

// TestMaskingPolicySQL pins CREATE, ALTER, and DROP MASKING POLICY and the catalog reads against
// r_CREATE_MASKING_POLICY, r_ALTER_MASKING_POLICY, r_DROP_MASKING_POLICY, and r_SVV_MASKING_POLICY.
func TestMaskingPolicySQL(t *testing.T) {
	basic := maskingTestPolicy("mask_email", "'***'::VARCHAR(256)")
	conditional := maskingTestPolicy("card_number_conditional_mask", "CASE WHEN fraudulent THEN REDACT_CREDIT_CARD(pan) ELSE NULL END",
		maskingPolicyColumn{Name: "fraudulent", Type: "BOOLEAN"}, maskingPolicyColumn{Name: "pan", Type: "varchar(16)"})
	defaults := maskingTestPolicy("mask_defaults", "NULL::VARCHAR(256)", maskingPolicyColumn{Name: "note", Type: "TEXT"}, maskingPolicyColumn{Name: "amount", Type: "decimal"}, maskingPolicyColumn{Name: "code", Type: "char"})
	quoted := maskingTestPolicy(`Odd"Policy`, `CASE WHEN "Odd""Column" LIKE 'it''s \%' THEN 'C:\masked' ELSE "Odd""Column" END`, maskingPolicyColumn{Name: `Odd"Column`, Type: "VARCHAR(64)"})
	commented := maskingTestPolicy("mask_comment", "'***'::VARCHAR(256) -- constant mask")
	changed := basic
	changed.Expression = types.StringValue("SHA2(email, 256)")
	invalid := func(change func(*maskingPolicyModel)) func() (string, error) {
		model := basic
		change(&model)
		return func() (string, error) { return createMaskingPolicyStatement(model) }
	}
	checkSQL(t, "masking_policy", []sqlCase{
		{"create_basic", func() (string, error) { return createMaskingPolicyStatement(basic) }},
		{"create_conditional", func() (string, error) { return createMaskingPolicyStatement(conditional) }},
		{"create_default_lengths", func() (string, error) { return createMaskingPolicyStatement(defaults) }},
		{"create_quoted", func() (string, error) { return createMaskingPolicyStatement(quoted) }},
		{"create_trailing_comment", func() (string, error) { return createMaskingPolicyStatement(commented) }},
		{"alter_expression", func() ([]string, error) { return alterMaskingPolicyStatements(basic, changed) }},
		{"alter_quoted", func() ([]string, error) {
			moved := quoted
			moved.Expression = types.StringValue(`'it''s \ masked'::VARCHAR(64)`)
			return alterMaskingPolicyStatements(quoted, moved)
		}},
		{"alter_unchanged", func() ([]string, error) { return alterMaskingPolicyStatements(basic, basic) }},
		{"alter_invalid_expression", func() ([]string, error) {
			broken := basic
			broken.Expression = types.StringValue("'***'); DROP TABLE users; --")
			return alterMaskingPolicyStatements(basic, broken)
		}},
		{"drop", func() string { return dropMaskingPolicyStatement(basic) }},
		{"drop_quoted", func() string { return dropMaskingPolicyStatement(quoted) }},
		{"read", maskingQuerySQL(readMaskingPolicyQuery(quoted))},
		{"list_all", maskingQuerySQL(listMaskingPoliciesQuery(""))},
		{"list_database", maskingQuerySQL(listMaskingPoliciesQuery("analytics"))},
		{"error_no_columns", invalid(func(m *maskingPolicyModel) { m.InputColumn = maskingPolicyColumnList(nil) })},
		{"error_duplicate_column", invalid(func(m *maskingPolicyModel) {
			m.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "TEXT"}, {Name: "EMAIL", Type: "TEXT"}})
		})},
		{"error_empty_column", invalid(func(m *maskingPolicyModel) {
			m.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "", Type: "TEXT"}})
		})},
		{"error_type", invalid(func(m *maskingPolicyModel) {
			m.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "TEXT; DROP TABLE users"}})
		})},
		{"error_type_anyelement", invalid(func(m *maskingPolicyModel) {
			m.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "ANYELEMENT"}})
		})},
		{"error_type_refcursor", invalid(func(m *maskingPolicyModel) {
			m.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "refcursor"}})
		})},
		{"error_statement_separator", invalid(func(m *maskingPolicyModel) { m.Expression = types.StringValue("'x'; DROP TABLE users") })},
		{"error_unterminated_literal", invalid(func(m *maskingPolicyModel) { m.Expression = types.StringValue("'it''s") })},
		{"error_unbalanced_parenthesis", invalid(func(m *maskingPolicyModel) { m.Expression = types.StringValue("'x')") })},
		{"error_empty_name", invalid(func(m *maskingPolicyModel) { m.Name = types.StringValue("") })},
	})
}

// TestMaskingPolicyCatalogParsing decodes the documented JSON forms and tolerates plain expression text.
func TestMaskingPolicyCatalogParsing(t *testing.T) {
	columns, err := maskingPolicyParseColumns(`[{"colname":"credit_card","type":"character varying(256)"},{"colname":"flag","type":"boolean"}]`)
	require.NoError(t, err)
	assert.Equal(t, []maskingPolicyColumn{{Name: "credit_card", Type: "CHARACTER VARYING(256)"}, {Name: "flag", Type: "BOOLEAN"}}, columns)
	_, err = maskingPolicyParseColumns("not json")
	require.Error(t, err)

	for text, expected := range map[string]string{
		`[{"expr":"SHA2((\"masked_table\".\"credit_card\" + CAST('testSalt' AS TEXT)), CAST(256 AS INT4))","type":"text"}]`: `SHA2(("masked_table"."credit_card" + CAST('testSalt' AS TEXT)), CAST(256 AS INT4))`,
		`[{"expr":"a","type":"text"},{"expr":"b","type":"integer"}]`:                                                        "a, b",
		`CAST('***' AS TEXT)`:              `CAST('***' AS TEXT)`,
		`[]`:                               `[]`,
		`[{"type":"text"}]`:                `[{"type":"text"}]`,
		`{"expr":"not a list of outputs"}`: `{"expr":"not a list of outputs"}`,
	} {
		assert.Equal(t, expected, maskingPolicyExpressionText(text), text)
	}

	configured := []maskingPolicyColumn{{Name: "Email", Type: "TEXT"}, {Name: "amount", Type: "decimal(10, 2)"}}
	assert.True(t, maskingPolicyColumnsMatch(configured, []maskingPolicyColumn{{Name: "email", Type: "character varying(256)"}, {Name: "amount", Type: "numeric(10,2)"}}))
	assert.False(t, maskingPolicyColumnsMatch(configured, []maskingPolicyColumn{{Name: "email", Type: "character varying(64)"}, {Name: "amount", Type: "numeric(10,2)"}}))
	assert.False(t, maskingPolicyColumnsMatch(configured, configured[:1]))
	assert.Equal(t, "SOME FUTURE TYPE", maskingPolicyCanonicalType("Some  Future TYPE"))
}
