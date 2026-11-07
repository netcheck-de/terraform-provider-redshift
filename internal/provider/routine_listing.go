package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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

// routineCollectionFilters are the filters of the function, external function, and procedure listings.
func routineCollectionFilters(noun string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database": discoveryDatabaseFilter(noun),
		"schema":   discoveryFilter("Only " + noun + " in this schema."),
		"name":     discoveryFilter("Only " + noun + " with this exact name, including every overload."),
	}
}

// routineCollectionDescriptions describe the listed values of a function or procedure, replacing resource text about
// configuration and apply behavior.
func routineCollectionDescriptions(noun string) map[string]string {
	return map[string]string{
		"database":    "Database containing the " + noun + ".",
		"schema":      "Schema containing the " + noun + ".",
		"name":        "Name of the " + noun + "; overloads share a name.",
		"arguments":   "Input argument types in order, as the catalog reports them without modifiers but in uppercase, such as `INTEGER` or `CHARACTER VARYING`; empty for a " + noun + " without arguments. With the name they identify the overload.",
		"signature":   "Input argument types as one comma-separated string, such as `INTEGER, CHARACTER VARYING`, as `ALTER`, `DROP`, and `GRANT` identify the overload; empty without arguments.",
		"return_type": "Result type, as the catalog reports it without modifiers but in uppercase, such as `INTEGER` or `CHARACTER VARYING`.",
		"volatility":  "`VOLATILE`, `STABLE`, or `IMMUTABLE`.",
		"owner":       "SQL user owning the " + noun + "; empty when the catalog no longer resolves the owner.",
	}
}

// routineCollectionElement derives a listing's element from the paired resource, describing the listed values.
func routineCollectionElement(factory func() resource.Resource, descriptions map[string]string, names ...string) map[string]schema.Attribute {
	selected := map[string]string{}

	for _, name := range names {
		if description, ok := descriptions[name]; ok {
			selected[name] = description
		}
	}

	return describedOutputs(collectionElement(factory, names...), selected)
}

// routineCollectionItem converts the identity and owner of one listed function or procedure from its catalog row.
func routineCollectionItem(database string, row sqlclient.Row) map[string]attr.Value {
	return map[string]attr.Value{
		"database": types.StringValue(database),
		"schema":   types.StringValue(row["schema_name"]),
		"name":     types.StringValue(row["routine_name"]),
		"owner":    types.StringValue(row["owner"]),
	}
}

// functionCollectionItem converts one listed function, SQL or Lambda, with its argument types, result type, and
// volatility spelled as the paired resources read them.
func functionCollectionItem(database string, row sqlclient.Row) (map[string]attr.Value, error) {
	volatility, err := routineVolatility(row["volatility"])
	if err != nil {
		return nil, fmt.Errorf("function %s.%s: %w", row["schema_name"], row["routine_name"], err)
	}

	item := routineCollectionItem(database, row)
	item["arguments"] = externalFunctionTypeValues(externalFunctionSignatureTypes(routineCatalogSignature(row["arguments"])))
	item["return_type"] = types.StringValue(routineCatalogType(row["return_type"]))
	item["volatility"] = types.StringValue(volatility)

	return item, nil
}

// routineListingRows runs a routine listing query with the given conditions in the selected database; a database
// that does not exist or is not local yields no rows.
func routineListingRows(ctx context.Context, client *resourceClient, filters types.Object, nameFilter string, conditions ...sqlclient.Keyword) (string, []sqlclient.Row, error) {
	database, local, err := discoveryLocalDatabase(ctx, client, filters, "database")
	if err != nil || !local {
		return database, nil, err
	}

	rows, err := client.selectRows(ctx, database, routineListingQuery(objectString(filters, "schema"), objectString(filters, nameFilter), conditions...))

	return database, rows, err
}

// routineListingInt parses an integer catalog column, failing on values that would otherwise read as zero.
func routineListingInt(row sqlclient.Row, column string) (int64, error) {
	value, err := strconv.ParseInt(row[column], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("catalog column %s has invalid integer %q", column, row[column])
	}

	return value, nil
}
