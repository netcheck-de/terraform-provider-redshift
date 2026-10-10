package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newColumnsDataSource)

// newColumnsDataSource lists relation columns with their types, nullability, and defaults.
func newColumnsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "columns",
		description: "Lists the columns of the tables, views, and external tables of one database visible to the provider's SQL identity, from `SVV_ALL_COLUMNS`, ordered by schema, table, and position.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("columns"),
			"schema":   discoveryFilter("Only columns of relations in this schema."),
			"table":    discoveryFilter("Only columns of relations with this name."),
		},
		element: map[string]schema.Attribute{
			"database":                 discoveryComputed("string", "Database containing the relation."),
			"schema":                   discoveryComputed("string", "Schema containing the relation."),
			"table":                    discoveryComputed("string", "Relation containing the column."),
			"name":                     discoveryComputed("string", "Column name."),
			"ordinal_position":         discoveryComputed("int64", "Position of the column in the relation, starting at 1."),
			"data_type":                discoveryComputed("string", "Data type name as the catalog reports it, such as `integer` or `character varying`, without length or precision."),
			"character_maximum_length": discoveryComputed("int64", "Maximum length of a character column; null for other types."),
			"numeric_precision":        discoveryComputed("int64", "Numeric precision as the catalog reports it: decimal digits for `DECIMAL`/`NUMERIC`, bits for integer types (for example 32 for `INTEGER` and 16 for `SMALLINT`); null for non-numeric types."),
			"numeric_scale":            discoveryComputed("int64", "Numeric scale as the catalog reports it: digits after the decimal point for `DECIMAL`/`NUMERIC`, 0 for integer types; null for non-numeric types."),
			"nullable":                 discoveryComputed("bool", "Whether the column accepts NULL; null when the catalog has no information."),
			"default":                  discoveryComputed("string", "Default expression; null when the column has none."),
			"remarks":                  discoveryComputed("string", "Comment on the column; null when it has none."),
		},
		list: columnsList,
	})
}

// columnsList reads the columns of the selected database from the administration database.
func columnsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := discoveryDatabase(client, filters)
	rows, err := client.selectRows(ctx, client.database.ValueString(), readColumnsQuery(database, objectString(filters, "schema"), objectString(filters, "table")))
	if err != nil {
		return nil, err
	}
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		item, err := columnsItem(database, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// columnsItem converts one SVV_ALL_COLUMNS row; a malformed number is an error rather than a silent null.
func columnsItem(database string, row map[string]string) (map[string]attr.Value, error) {
	if row["schema_name"] == "" || row["table_name"] == "" || row["column_name"] == "" {
		return nil, fmt.Errorf("SVV_ALL_COLUMNS returned a column without a schema, table, or name in database %q", database)
	}
	item := map[string]attr.Value{
		"database":  types.StringValue(database),
		"schema":    types.StringValue(row["schema_name"]),
		"table":     types.StringValue(row["table_name"]),
		"name":      types.StringValue(row["column_name"]),
		"data_type": discoveryText(row["data_type"]),
		"default":   discoveryText(row["column_default"]),
		"remarks":   discoveryText(row["remarks"]),
	}
	for _, column := range []string{"ordinal_position", "character_maximum_length", "numeric_precision", "numeric_scale"} {
		value, err := discoveryInt64(column, row[column])
		if err != nil {
			return nil, err
		}
		item[column] = value
	}
	// The catalog may report an empty string when it has no nullability information.
	switch row["is_nullable"] {
	case "YES":
		item["nullable"] = types.BoolValue(true)
	case "NO":
		item["nullable"] = types.BoolValue(false)
	default:
		item["nullable"] = types.BoolNull()
	}
	return item, nil
}
