package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestKeywordChanged replaces only for a different keyword, not for another case of the same one.
func TestKeywordChanged(t *testing.T) {
	for _, test := range []struct {
		before, after string
		unknown       bool
		replace       bool
	}{
		{before: "SQL", after: "sql"},
		{before: "awsidc", after: "AWSIDC"},
		{before: "AWSIDC", after: "AZURE", replace: true},
		{before: "SQL", unknown: true, replace: true},
	} {
		after := types.StringValue(test.after)
		if test.unknown {
			after = types.StringUnknown()
		}
		var resp stringplanmodifier.RequiresReplaceIfFuncResponse
		keywordChanged(context.Background(), planmodifier.StringRequest{StateValue: types.StringValue(test.before), PlanValue: after}, &resp)
		assert.Equal(t, test.replace, resp.RequiresReplace, "%s -> %s", test.before, test.after)
	}
}

// TestKeywordValue keeps the configured case of a matching keyword and otherwise reports the canonical catalog value.
func TestKeywordValue(t *testing.T) {
	assert.Equal(t, types.StringValue("iam"), keywordValue(types.StringValue("iam"), "IAM"))
	assert.Equal(t, types.StringValue("MTLS"), keywordValue(types.StringValue("iam"), "MTLS"))
	assert.Equal(t, types.StringValue("IAM"), keywordValue(types.StringNull(), "IAM"))
	assert.Equal(t, types.StringValue("IAM"), keywordValue(types.StringUnknown(), "IAM"))
	assert.Equal(t, types.StringValue("MTLS"), keywordCanonical(types.StringValue("mTLS")))
	assert.True(t, keywordCanonical(types.StringNull()).IsNull())
	assert.True(t, keywordCanonical(types.StringUnknown()).IsUnknown())
}

// TestAssumeroleGrantRoleChanged records another case of DEFAULT or ALL in place, but replaces for a different
// selector or any change to an ARN, which is case-sensitive.
func TestAssumeroleGrantRoleChanged(t *testing.T) {
	const role = "arn:aws:iam::123456789012:role/Loader"
	for _, test := range []struct {
		before, after string
		replace       bool
	}{
		{before: "default", after: "DEFAULT"},
		{before: "ALL", after: "all"},
		{before: "DEFAULT", after: "ALL", replace: true},
		{before: role, after: "arn:aws:iam::123456789012:role/loader", replace: true},
		{before: "DEFAULT", after: role, replace: true},
	} {
		var resp stringplanmodifier.RequiresReplaceIfFuncResponse
		assumeroleGrantRoleChanged(context.Background(), planmodifier.StringRequest{StateValue: types.StringValue(test.before), PlanValue: types.StringValue(test.after)}, &resp)
		assert.Equal(t, test.replace, resp.RequiresReplace, "%s -> %s", test.before, test.after)
	}
	fields := map[string]string{"iam_role_arn": "Default"}
	assumeroleGrantCanonical(fields)
	assert.Equal(t, "DEFAULT", fields["iam_role_arn"])
	fields["iam_role_arn"] = role
	assumeroleGrantCanonical(fields)
	assert.Equal(t, role, fields["iam_role_arn"])
}
