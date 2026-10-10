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

// newTablesDataSource lists the tables, views, and materialized views of one database.
func newTablesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "tables",
		description: "Lists the tables, views, materialized views, external tables, and datashare tables of one database visible to the provider's SQL identity, from `SVV_ALL_TABLES`, with owners from `SVV_REDSHIFT_TABLES` and materialized views identified through `SVV_MV_INFO`. Unless the identity is a superuser, `SVV_MV_INFO` shows only its own materialized views, so materialized views owned by other users are listed as `VIEW`.",
		filters: map[string]schema.Attribute{
			"database":   discoveryDatabaseFilter("relations"),
			"schema":     discoveryFilter("Only relations in this schema."),
			"table_type": discoveryFilter("Only relations of this type: `TABLE`, `VIEW` (ordinary and late-binding views), `MATERIALIZED VIEW`, `EXTERNAL TABLE`, or `SHARED TABLE`.", tablesTypes...),
		},
		element: map[string]schema.Attribute{
			"database":   discoveryComputed("string", "Database containing the relation."),
			"schema":     discoveryComputed("string", "Schema containing the relation."),
			"name":       discoveryComputed("string", "Relation name."),
			"table_type": discoveryComputed("string", "Relation type in upper case: `TABLE`, `VIEW`, `MATERIALIZED VIEW`, `EXTERNAL TABLE`, or another type the catalog reports, such as `SHARED TABLE`."),
			"owner":      discoveryComputed("string", "SQL user owning the relation; null for external tables."),
			"remarks":    discoveryComputed("string", "Comment on the relation; null when it has none."),
		},
		list: tablesList,
	})
}

// tablesList reads the relations of the selected database from the administration database and reclassifies the
// views that are materialized.
func tablesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, schemaName, tableType := discoveryDatabase(client, filters), objectString(filters, "schema"), objectString(filters, "table_type")
	rows, err := client.selectRows(ctx, client.database.ValueString(), readTablesQuery(database, schemaName, tableType))
	if err != nil {
		return nil, err
	}
	materialized := map[[2]string]bool{}
	if tablesNeedMaterialized(tableType) && len(rows) > 0 {
		views, err := client.selectRows(ctx, client.database.ValueString(), readTablesMaterializedQuery(database, schemaName))
		if err != nil {
			return nil, err
		}
		for _, view := range views {
			materialized[[2]string{view["schema_name"], view["name"]}] = true
		}
	}
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		if row["schema_name"] == "" || row["table_name"] == "" {
			return nil, fmt.Errorf("SVV_ALL_TABLES returned a relation without a schema or name in database %q", database)
		}
		kind := row["table_type"]
		if kind == tablesTypeView && materialized[[2]string{row["schema_name"], row["table_name"]}] {
			kind = tablesTypeMaterialized
		}
		if tableType != "" && kind != tableType {
			continue
		}
		items = append(items, map[string]attr.Value{
			"database":   types.StringValue(database),
			"schema":     types.StringValue(row["schema_name"]),
			"name":       types.StringValue(row["table_name"]),
			"table_type": discoveryText(kind),
			"owner":      discoveryText(row["owner"]),
			"remarks":    discoveryText(row["remarks"]),
		})
	}
	return items, nil
}
