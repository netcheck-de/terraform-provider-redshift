package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// discoveryFilter defines an optional listing filter; choices restrict it to the catalog's documented values.
func discoveryFilter(description string, choices ...string) schema.StringAttribute {
	validators := []validator.String{stringvalidator.LengthAtLeast(1)}
	if len(choices) != 0 {
		validators = append(validators, stringvalidator.OneOfCaseInsensitive(choices...))
	}
	return schema.StringAttribute{Optional: true, MarkdownDescription: description, Validators: validators}
}

// discoveryDatabaseFilter is the database filter shared by listings scoped to one database.
func discoveryDatabaseFilter(objects string) schema.StringAttribute {
	return discoveryFilter("Database whose " + objects + " are listed; defaults to the provider's `database`.")
}

// discoveryComputed defines a computed element attribute of the given type.
func discoveryComputed(kind string, description string) schema.Attribute {
	switch kind {
	case "int64":
		return schema.Int64Attribute{Computed: true, MarkdownDescription: description}
	case "bool":
		return schema.BoolAttribute{Computed: true, MarkdownDescription: description}
	case "list":
		return schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: description}
	default:
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
}

// discoveryDatabase returns the database a listing reads: the database filter, else the provider database.
func discoveryDatabase(client *resourceClient, filters types.Object) string {
	if database := objectString(filters, "database"); database != "" {
		return database
	}
	return client.database.ValueString()
}

// discoveryText maps an absent catalog value to null. Both transports report SQL NULL as "", so an empty string
// cannot be told apart from NULL and is treated as absent.
func discoveryText(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// discoveryInt64 parses an optional integer column; NULL becomes null.
func discoveryInt64(column, value string) (types.Int64, error) {
	if value == "" {
		return types.Int64Null(), nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return types.Int64Null(), fmt.Errorf("catalog column %s has non-integer value %q", column, value)
	}
	return types.Int64Value(parsed), nil
}
