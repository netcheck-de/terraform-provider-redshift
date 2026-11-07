package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newDatashareSchemasDataSource)

// datashareListVisibility documents the row visibility of SVV_DATASHARE_OBJECTS that every member listing inherits.
const datashareListVisibility = "Superusers see the members of every share; other users see only rows visible to them in `SVV_DATASHARE_OBJECTS` and `SVV_DATASHARES`."

// datashareObjectFilters are the database and share filters of the member listings.
func datashareObjectFilters() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database":  discoveryDatabaseFilter("outbound share members"),
		"datashare": discoveryFilter("Only members of this outbound datashare; all outbound shares of the database when omitted."),
	}
}

// listDatashareObjects reads the outbound share members of the selected database that match objectTypes. A missing
// database yields no rows instead of an error.
func listDatashareObjects(ctx context.Context, client *resourceClient, filters types.Object, objectTypes sqlclient.Keyword) (string, []sqlclient.Row, error) {
	database := discoveryDatabase(client, filters)

	exists, err := client.localDatabaseExists(ctx, database)
	if err != nil || !exists {
		return database, nil, err
	}

	rows, err := client.selectRows(ctx, database, listDatashareObjectsQuery(database, objectString(filters, "datashare"), objectTypes))

	return database, rows, err
}

// datashareListedSchema reports whether a member row is a shared schema.
func datashareListedSchema(row sqlclient.Row) bool {
	return row["object_type"] == "schema" || row["object_type"] == "schemas"
}

// newDatashareSchemasDataSource lists the schemas of outbound datashares in one producer database.
func newDatashareSchemasDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "datashare_schemas",
		databaseField: "database",
		description:   "Lists the schemas of the outbound datashares in one producer database from `SVV_DATASHARE_OBJECTS`, without taking ownership. " + datashareListVisibility,
		filters:       datashareObjectFilters(),
		element: describedOutputs(collectionElement(newDatashareSchemaResource, "database", "datashare", "schema", "include_new"), map[string]string{
			"schema":      "Schema included in the share.",
			"include_new": "Whether future objects created in the schema are added to the share automatically.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := listDatashareObjects(ctx, client, filters, datashareSchemaTypes)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				includeNew := types.BoolNull()

				if text := strings.TrimSpace(row["include_new"]); text != "" {
					value, err := strconv.ParseBool(text)
					if err != nil {
						return nil, fmt.Errorf("decode datashare include_new: %w", err)
					}

					includeNew = types.BoolValue(value)
				}

				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "datashare": types.StringValue(strings.TrimSpace(row["share_name"])),
					"schema": types.StringValue(row["object_name"]), "include_new": includeNew,
				})
			}

			return items, nil
		},
	})
}
