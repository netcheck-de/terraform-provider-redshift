package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// routineListingFilters are the optional filters every routine listing accepts.
func routineListingFilters(nameFilter, noun string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database to list " + noun + " from; defaults to the provider database."},
		"schema":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only " + noun + " in this schema."},
		nameFilter: schema.StringAttribute{Optional: true, MarkdownDescription: "Only " + noun + " with this exact name, including every overload."},
	}
}

// routineListingElement holds the attributes shared by listed functions and procedures.
func routineListingElement(nameAttribute, noun string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database":    schema.StringAttribute{Computed: true, MarkdownDescription: "Database containing the " + noun + "."},
		"schema":      schema.StringAttribute{Computed: true, MarkdownDescription: "Schema containing the " + noun + "."},
		nameAttribute: schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the " + noun + "."},
		"arguments":   schema.ListAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: "Input argument types in order, as the catalog reports them without length or precision but in uppercase, such as `INTEGER` or `CHARACTER VARYING`; with the name they identify the overload."},
		"language":    schema.StringAttribute{Computed: true, MarkdownDescription: "Implementation language in uppercase, such as `SQL`, `PLPYTHONU`, `PLPGSQL`, or `EXFUNC` for Lambda UDFs."},
		"owner":       schema.StringAttribute{Computed: true, MarkdownDescription: "SQL user owning the " + noun + "; empty when the catalog no longer resolves the owner."},
	}
}

// routineListingDatabase returns the database a listing reads: its database filter, or the provider database.
func routineListingDatabase(client *resourceClient, filters types.Object) string {
	if selected := objectString(filters, "database"); selected != "" {
		return selected
	}
	return client.database.ValueString()
}

// routineListingRows runs a routine listing query in the selected database.
func routineListingRows(ctx context.Context, client *resourceClient, filters types.Object, kind sqlclient.Keyword, nameFilter string) (string, []sqlclient.Row, error) {
	database := routineListingDatabase(client, filters)
	rows, err := client.selectRows(ctx, database, routineListingQuery(kind, objectString(filters, "schema"), objectString(filters, nameFilter)))
	return database, rows, err
}

// routineListingItem converts the attributes shared by functions and procedures from one catalog row.
func routineListingItem(database, nameAttribute string, row sqlclient.Row) map[string]attr.Value {
	return map[string]attr.Value{
		"database":    types.StringValue(database),
		"schema":      types.StringValue(row["schema_name"]),
		nameAttribute: types.StringValue(row["routine_name"]),
		"arguments":   externalFunctionTypeValues(externalFunctionSignatureTypes(routineCatalogSignature(row["arguments"]))),
		"language":    types.StringValue(strings.ToUpper(row["language"])),
		"owner":       types.StringValue(row["owner"]),
	}
}

// routineListingInt parses an integer catalog column, failing on values that would otherwise read as zero.
func routineListingInt(row sqlclient.Row, column string) (int64, error) {
	value, err := strconv.ParseInt(row[column], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("catalog column %s has invalid integer %q", column, row[column])
	}
	return value, nil
}
