package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newTablesDataSource)

// newTablesDataSource lists the local tables of one database, the objects redshift_table manages.
func newTablesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "tables",
		databaseField: "database",
		description: "Lists the local tables of one database from `SVV_REDSHIFT_TABLES`, without views, materialized views, external tables, or system tables. " +
			"Superusers see every table; other users see only the tables they have access to. A database that is not local, such as a datashare consumer database, lists no tables.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("tables"),
			"schema":   discoveryFilter("Only tables in this schema."),
		},
		element: describedOutputs(collectionElement(newTableResource, "database", "schema", "name", "owner"), map[string]string{
			"owner": "User owning the table; null when the catalog reports none.",
		}),
		list: tablesList,
	})
}

// tablesList reads the tables of the selected local database. SVV_REDSHIFT_TABLES covers every database, so it runs
// in the provider database.
func tablesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := discoveryDatabase(client, filters)

	local, err := client.localDatabaseExists(ctx, database)
	if err != nil || !local {
		return []map[string]attr.Value{}, err
	}

	rows, err := client.selectRows(ctx, client.database.ValueString(), listTablesQuery(database, objectString(filters, "schema")))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		if row["schema_name"] == "" || row["table_name"] == "" {
			return nil, fmt.Errorf("SVV_REDSHIFT_TABLES returned a table without a schema or name in database %q", database)
		}

		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database),
			"schema":   types.StringValue(row["schema_name"]),
			"name":     types.StringValue(row["table_name"]),
			"owner":    discoveryText(row["table_owner"]),
		})
	}

	return items, nil
}
