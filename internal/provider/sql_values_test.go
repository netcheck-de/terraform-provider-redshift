package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKnownScalars maps null and unknown values to the zero value that the Opt* builders skip.
func TestKnownScalars(t *testing.T) {
	assert.Equal(t, "analytics", knownString(types.StringValue("analytics")))
	assert.Empty(t, knownString(types.StringNull()))
	assert.Empty(t, knownString(types.StringUnknown()))

	limit := knownInt64(types.Int64Value(0))
	require.NotNil(t, limit, "zero is a configured value, not an absent one")
	assert.Equal(t, int64(0), *limit)
	assert.Nil(t, knownInt64(types.Int64Null()))
	assert.Nil(t, knownInt64(types.Int64Unknown()))

	enabled := knownBool(types.BoolValue(false))
	require.NotNil(t, enabled, "false is a configured value, not an absent one")
	assert.False(t, *enabled)
	assert.Nil(t, knownBool(types.BoolNull()))
	assert.Nil(t, knownBool(types.BoolUnknown()))
}

// TestKnownStrings sorts the known elements of lists and sets and drops null or unknown elements.
func TestKnownStrings(t *testing.T) {
	elements := []attr.Value{types.StringValue("UPDATE"), types.StringUnknown(), types.StringValue("DELETE"), types.StringNull(), types.StringValue("")}
	assert.Equal(t, []string{"", "DELETE", "UPDATE"}, knownStrings(types.ListValueMust(types.StringType, elements)))
	assert.Equal(t, []string{"INSERT", "SELECT"}, knownStrings(types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT"), types.StringValue("INSERT")})))
	assert.Nil(t, knownStrings(types.SetNull(types.StringType)))
	assert.Nil(t, knownStrings(types.ListUnknown(types.StringType)))
	assert.Empty(t, knownStrings(types.SetValueMust(types.StringType, nil)))
}

// TestKnownMap keeps known string entries and treats a null or unknown map as absent.
func TestKnownMap(t *testing.T) {
	value := types.MapValueMust(types.StringType, map[string]attr.Value{
		"search_path": types.StringValue("public"), "timezone": types.StringUnknown(), "datestyle": types.StringNull(), "empty": types.StringValue(""),
	})
	assert.Equal(t, map[string]string{"search_path": "public", "empty": ""}, knownMap(value))
	assert.Nil(t, knownMap(types.MapNull(types.StringType)))
	assert.Nil(t, knownMap(types.MapUnknown(types.StringType)))
	assert.Empty(t, knownMap(types.MapValueMust(types.StringType, nil)))
}

// TestOptOneOf returns the allowlisted spelling, nothing for an unset value, and an error for anything else.
func TestOptOneOf(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    types.String
		expected sqlclient.Keyword
	}{
		{"canonical spelling", types.StringValue("serializable"), "SERIALIZABLE"},
		{"null", types.StringNull(), ""},
		{"unknown", types.StringUnknown(), ""},
		{"empty", types.StringValue(""), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			keyword, err := optOneOf(test.value, "SERIALIZABLE", "SNAPSHOT")
			require.NoError(t, err)
			assert.Equal(t, test.expected, keyword)
		})
	}
	keyword, err := optOneOf(types.StringValue("SNAPSHOT; DROP TABLE t"), "SERIALIZABLE", "SNAPSHOT")
	require.ErrorContains(t, err, "is not one of SERIALIZABLE, SNAPSHOT")
	assert.Empty(t, keyword)
}
