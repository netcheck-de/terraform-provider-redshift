package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatabasesDataSource)

// newDatabasesDataSource lists the warehouse's local and datashare databases.
func newDatabasesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "databases",
		description: "Lists the local and datashare databases visible to the provider's SQL identity, from `SVV_REDSHIFT_DATABASES`. AWS Glue Data Catalog databases are not included.",
		filters: map[string]schema.Attribute{
			"database_type": discoveryFilter("Only databases of this type: `local`, or `shared` for databases created from a datashare.", "local", "shared"),
			"name_like":     discoveryFilter("Only databases whose name matches this case-sensitive SQL `LIKE` pattern, where `%` matches any sequence and `_` any single character."),
		},
		element: map[string]schema.Attribute{
			"name":            discoveryComputed("string", "Database name."),
			"owner":           discoveryComputed("string", "SQL user owning the database; null when the owner is not a local user."),
			"database_type":   discoveryComputed("string", "`local` or `shared`."),
			"isolation_level": discoveryComputed("string", "Isolation level as the catalog reports it, such as `Snapshot Isolation` or `Serializable`; null when unknown."),
		},
		list: databasesList,
	})
}

// databasesList reads the databases matching the filters from the administration database, because
// SVV_REDSHIFT_DATABASES covers every database on the warehouse.
func databasesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	rows, err := client.selectRows(ctx, client.database.ValueString(), readDatabasesQuery(objectString(filters, "database_type"), objectString(filters, "name_like")))
	if err != nil {
		return nil, err
	}
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		if row["database_name"] == "" {
			return nil, fmt.Errorf("SVV_REDSHIFT_DATABASES returned a database without a name")
		}
		items = append(items, map[string]attr.Value{
			"name":            types.StringValue(row["database_name"]),
			"owner":           discoveryText(row["owner"]),
			"database_type":   discoveryText(row["database_type"]),
			"isolation_level": discoveryText(row["isolation_level"]),
		})
	}
	return items, nil
}
