package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopySHOWControlEscapes decodes the backslash escapes SHOW prints in COPY options exactly once and keeps
// configured values verbatim, so control characters and literal backslashes stay distinct.
func TestCopySHOWControlEscapes(t *testing.T) {
	for text, expected := range map[string]string{
		`DELIMITER '\t'`:   "\t",
		`DELIMITER '\n'`:   "\n",
		`DELIMITER '\r'`:   "\r",
		`DELIMITER '\\t'`:  `\t`,
		`NULL '\\n'`:       `\n`,
		`NULL '\\\\n'`:     `\\n`,
		`NULL '\\'`:        `\`,
		`NULL '\N'`:        `\N`,
		`NULL 'O''Reilly'`: "O'Reilly",
	} {
		tokens, err := sqlclient.Tokens(text)
		require.NoError(t, err)
		definition, err := parseCopyOptions(tokens)
		require.NoError(t, err)
		for _, value := range definition.parameters {
			assert.Equal(t, expected, value, text)
		}
	}
	configured := templateTestModel("", "", "", map[string]string{"DELIMITER": "\t"}).Parameters
	assert.True(t, copyParametersValue(configured, map[string]string{"DELIMITER": "\t"}, false).Equal(configured))
	for _, value := range []string{"\t", "\n", `\t`, `\n`, `\\t`, `\\n`, `\`, `O'Reilly\tail`} {
		_, parameter, err := copyParameterKeyword("NULL", false)
		require.NoError(t, err)
		canonical, err := copyCanonicalValue("NULL", parameter, value)
		require.NoError(t, err)
		assert.Equal(t, value, canonical, "configured text is not decoded")
		tokens, err := sqlclient.Tokens("NULL AS " + sqlclient.Literal(value))
		require.NoError(t, err)
		definition, err := parseCopyOptions(tokens)
		require.NoError(t, err)
		assert.Equal(t, value, definition.parameters["NULL"], "rendered literals decode exactly once")
	}
}

// TestCopyParameterScopes keeps the template-only and job-only parameters apart, as CREATE TEMPLATE and COPY JOB
// document them.
func TestCopyParameterScopes(t *testing.T) {
	templates, jobs := copyParameterNames(false), copyParameterNames(true)
	for _, name := range []string{"MAXERROR", "IGNOREALLERRORS"} {
		assert.Contains(t, templates, name)
		assert.NotContains(t, jobs, name)
	}
	assert.Contains(t, jobs, "REGION")
	assert.NotContains(t, templates, "REGION")
	for _, excluded := range []string{"MANIFEST", "IAM_ROLE", "CREDENTIALS", "ACCESS_KEY_ID", "FORMAT", "CSV", "JSON"} {
		assert.NotContains(t, templates, excluded)
		assert.NotContains(t, jobs, excluded)
	}
	assert.Equal(t, []string{"AVRO", "CSV", "FIXEDWIDTH", "JSON", "ORC", "PARQUET", "SHAPEFILE"}, copyFormatNames())
	for keyword, parameter := range copyParameters {
		assert.Equal(t, parameter.argument == copyChoice, len(parameter.choices) > 0, keyword)
	}
}

// TestCopyParametersValue keeps configured spellings while they equal the catalog and surfaces catalog values
// otherwise.
func TestCopyParametersValue(t *testing.T) {
	configured := templateTestModel("", "", "", map[string]string{"IGNOREHEADER": "01", "ENCODING": "utf16", "GZIP": ""}).Parameters
	catalog := map[string]string{"IGNOREHEADER": "1", "ENCODING": "UTF16", "GZIP": ""}
	assert.True(t, copyParametersValue(configured, catalog, false).Equal(configured))

	drifted := map[string]string{"IGNOREHEADER": "1", "ENCODING": "UTF8"}
	assert.Equal(t, map[string]string{"IGNOREHEADER": "01", "ENCODING": "UTF8"}, knownMap(copyParametersValue(configured, drifted, false)))

	assert.True(t, copyParametersValue(types.MapNull(types.StringType), map[string]string{}, false).IsNull())
	assert.True(t, copyParametersValue(types.Map{}, map[string]string{}, true).Equal(types.MapNull(types.StringType)))
	assert.True(t, copyParametersValue(types.MapUnknown(types.StringType), map[string]string{}, true).Equal(types.MapNull(types.StringType)))
	empty := types.MapValueMust(types.StringType, nil)
	assert.True(t, copyParametersValue(empty, map[string]string{}, false).Equal(empty))
	assert.True(t, copyParametersValue(configured, map[string]string{}, false).IsNull(), "parameters dropped outside Terraform surface as none")
	assert.Equal(t, catalog, knownMap(copyParametersValue(types.MapNull(types.StringType), catalog, false)), "an import reads the catalog")
}

// TestCopyCanonicalValue validates every argument kind.
func TestCopyCanonicalValue(t *testing.T) {
	for _, test := range []struct {
		name, value, canonical string
		valid                  bool
	}{
		{"GZIP", "", "", true},
		{"GZIP", "true", "", false},
		{"IGNOREHEADER", "007", "7", true},
		{"IGNOREHEADER", "-1", "", false},
		{"COMPUPDATE", "preset", "PRESET", true},
		{"STATUPDATE", "PRESET", "", false},
		{"ACCEPTINVCHARS", "", "", true},
		{"ACCEPTINVCHARS", "?", "?", true},
		{"DELIMITER", "\\t", "\\t", true},
	} {
		_, parameter, err := copyParameterKeyword(test.name, false)
		require.NoError(t, err)
		canonical, err := copyCanonicalValue(test.name, parameter, test.value)
		assert.Equal(t, test.valid, err == nil, "%s=%q", test.name, test.value)
		if test.valid {
			assert.Equal(t, test.canonical, canonical, "%s=%q", test.name, test.value)
		}
	}
}

// TestCopyParameterEdges covers literal decoding, invalid parameters, SIMPLIFY forms, and unknown parameter values.
func TestCopyParameterEdges(t *testing.T) {
	assert.Equal(t, "decoded", copyLiteralValue(sqlclient.Token{Text: "decoded"}))
	assert.Equal(t, "\b\f\v", copyLiteralValue(sqlclient.Token{Raw: `'\b\f\v'`}))
	for _, parameters := range []map[string]string{{"MANIFEST": ""}, {"IGNOREHEADER": "bad"}} {
		_, err := copyCanonicalParameters(parameters, false)
		require.Error(t, err)
		_, err = copyParameterFragments(parameters, false)
		require.Error(t, err)
	}
	for _, argument := range []string{"", "SIMPLIFY -0"} {
		fragment, err := copySimplify(argument)
		require.NoError(t, err)
		assert.NotContains(t, fragment.String(), "-0")
	}
	assert.Equal(t, types.StringValue(""), copyFormatArgumentValue(types.StringValue(""), "CSV", ""))
	fragment, err := copyParameterFragment("ACCEPTINVCHARS", "", false)
	require.NoError(t, err)
	assert.Equal(t, "ACCEPTINVCHARS", fragment.String())
	for _, text := range []string{"SHAPEFILE SIMPLIFY SIMPLIFY", "SHAPEFILE SIMPLIFY 'bad'", "CSV SIMPLIFY"} {
		tokens, err := sqlclient.Tokens(text)
		require.NoError(t, err)
		_, err = parseCopyOptions(tokens)
		require.Error(t, err)
	}
	require.Error(t, copyValidateKnown(types.StringValue("bad"), types.StringUnknown(), types.MapNull(types.StringType), false))
	nullParameter := types.MapValueMust(types.StringType, map[string]attr.Value{"DELIMITER": types.StringNull()})
	require.Error(t, copyValidateKnown(types.StringNull(), types.StringNull(), nullParameter, false))
	unknownParameter := types.MapValueMust(types.StringType, map[string]attr.Value{"DELIMITER": types.StringUnknown()})
	assert.False(t, copyOptionsKnown(types.StringNull(), types.StringNull(), unknownParameter))
}
