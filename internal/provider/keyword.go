package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Keyword-like enum values, such as a language or an authentication method, are documented, rendered and reported
// in uppercase, but SQL keywords are case-insensitive, so configuration may spell them in any case. Comparisons
// ignore case, and state keeps the configured spelling.

// keywordChanged is a RequiresReplaceIf condition for a keyword-like attribute: another case of the same keyword
// is recorded in place instead of replacing the object.
func keywordChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = req.PlanValue.IsUnknown() || !strings.EqualFold(req.StateValue.ValueString(), req.PlanValue.ValueString())
}

// keywordCanonical uppercases a known keyword, so values that differ only in case compare equal.
func keywordCanonical(value types.String) types.String {
	if value.IsNull() || value.IsUnknown() {
		return value
	}
	return types.StringValue(strings.ToUpper(value.ValueString()))
}

// keywordValue keeps the configured spelling of a keyword the catalog reports in another case, and otherwise
// reports the catalog value, which callers pass in its canonical uppercase spelling.
func keywordValue(configured types.String, catalog string) types.String {
	if !configured.IsNull() && !configured.IsUnknown() && strings.EqualFold(configured.ValueString(), catalog) {
		return configured
	}
	return types.StringValue(catalog)
}
