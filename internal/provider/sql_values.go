package provider

import (
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// knownString returns the value, or "" when it is null or unknown, so the Opt* builders skip the clause.
func knownString(value types.String) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}
	return value.ValueString()
}

// knownInt64 returns a pointer to the value, or nil when it is null or unknown, for sqlclient.Statement.OptInt.
func knownInt64(value types.Int64) *int64 {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	known := value.ValueInt64()
	return &known
}

// knownBool returns a pointer to the value, or nil when it is null or unknown, for sqlclient.Statement.OptToggle.
func knownBool(value types.Bool) *bool {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	known := value.ValueBool()
	return &known
}

// stringElements is implemented by types.List and types.Set.
type stringElements interface {
	IsNull() bool
	IsUnknown() bool
	Elements() []attr.Value
}

// knownStrings returns the known string elements of a list or set, sorted so rendered SQL does not depend on
// set iteration order.
func knownStrings(value stringElements) []string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var strings []string
	for _, element := range value.Elements() {
		if text, ok := element.(types.String); ok && !text.IsNull() && !text.IsUnknown() {
			strings = append(strings, text.ValueString())
		}
	}
	slices.Sort(strings)
	return strings
}

// knownMap returns the known string elements of a map, or nil when the map is null or unknown.
func knownMap(value types.Map) map[string]string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	known := map[string]string{}
	for key, element := range value.Elements() {
		if text, ok := element.(types.String); ok && !text.IsNull() && !text.IsUnknown() {
			known[key] = text.ValueString()
		}
	}
	return known
}

// optOneOf maps an optional choice attribute to its allowlisted keyword, or "" when it is unset, so
// sqlclient.Statement.OptKw skips the clause.
func optOneOf(value types.String, allowed ...sqlclient.Keyword) (sqlclient.Keyword, error) {
	if text := knownString(value); text != "" {
		return sqlclient.OneOf(text, allowed...)
	}
	return "", nil
}
