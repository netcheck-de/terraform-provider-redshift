package provider

import (
	"context"
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

// discoveryGranteeTypeFilter narrows a grant listing to one grantee type, validated as the paired resource's
// grantee_type, so a user and a role of the same name can be told apart.
func discoveryGranteeTypeFilter() schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true, MarkdownDescription: "Only grants to `USER`, `ROLE`, `GROUP`, or `PUBLIC` identities.",
		Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.OneOf(grantGranteeTypes...)},
	}
}

// discoveryLocalScope documents the policy of listings whose objects exist only in local databases.
const discoveryLocalScope = "A database that does not exist or is not local lists nothing."

// discoverySharedScope documents the policy of listings that, like their resource, also read shared databases.
const discoverySharedScope = "A database that does not exist lists nothing; a shared (datashare consumer) database is read from the provider database."

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
	return discoveryDatabaseOf(client, filters, "database")
}

// discoveryDatabaseOf returns the database a listing reads from the named filter, else the provider database.
func discoveryDatabaseOf(client *resourceClient, filters types.Object, field string) string {
	if database := objectString(filters, field); database != "" {
		return database
	}

	return client.database.ValueString()
}

// discoveryLocalDatabase returns the database selected by the named filter and whether it is a local database.
// A database that does not exist or is shared from a datashare holds no local objects, so its listing is empty
// instead of failing to connect.
func discoveryLocalDatabase(ctx context.Context, client *resourceClient, filters types.Object, field string) (string, bool, error) {
	database := discoveryDatabaseOf(client, filters, field)
	local, err := client.localDatabaseExists(ctx, database)

	return database, local, err
}

// discoveryGrantTarget returns where the grants of database are read, as the grant resources read them: a local
// database in itself and a shared one from the provider database. ok is false for a database that does not exist.
func discoveryGrantTarget(ctx context.Context, client *resourceClient, database string) (string, bool, error) {
	admin := client.database.ValueString()

	rows, err := client.selectRows(ctx, admin, grantDatabaseTypeQuery(database))
	if err != nil || len(rows) == 0 {
		return "", false, err
	}

	if rows[0]["database_type"] == "local" {
		return database, true, nil
	}

	return admin, true, nil
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
