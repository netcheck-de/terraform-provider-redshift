package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
