package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newSchemasDataSource)

// newSchemasDataSource lists the local, external, and datashare schemas of one database.
func newSchemasDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "schemas",
		description: "Lists the schemas of one database visible to the provider's SQL identity, from `SVV_ALL_SCHEMAS`: local schemas, including system schemas such as `pg_catalog`, external schemas, and schemas of datashare databases.",
		filters: map[string]schema.Attribute{
			"database":    discoveryDatabaseFilter("schemas"),
			"schema_type": discoveryFilter("Only schemas of this type, in any case: `LOCAL`, `EXTERNAL`, or `SHARED`.", "LOCAL", "EXTERNAL", "SHARED"),
		},
		element: map[string]schema.Attribute{
			"database":        discoveryComputed("string", "Database containing the schema."),
			"name":            discoveryComputed("string", "Schema name."),
			"owner":           discoveryComputed("string", "SQL user owning the schema; null for shared schemas, whose owner belongs to the producer."),
			"schema_type":     discoveryComputed("string", "`LOCAL`, `EXTERNAL`, or `SHARED`."),
			"source_database": discoveryComputed("string", "Source database of an external schema, such as its AWS Glue database; null otherwise."),
		},
		list: schemasList,
	})
}

// schemasList reads the schemas of the selected database from the administration database, because
// SVV_ALL_SCHEMAS spans databases and datashare databases may not accept connections.
func schemasList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := discoveryDatabase(client, filters)
	rows, err := client.selectRows(ctx, client.database.ValueString(), readSchemasQuery(database, strings.ToLower(objectString(filters, "schema_type"))))
	if err != nil {
		return nil, err
	}
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		if row["schema_name"] == "" {
			return nil, fmt.Errorf("SVV_ALL_SCHEMAS returned a schema without a name in database %q", database)
		}
		items = append(items, map[string]attr.Value{
			"database":        types.StringValue(database),
			"name":            types.StringValue(row["schema_name"]),
			"owner":           discoveryText(row["owner"]),
			"schema_type":     discoveryText(strings.ToUpper(row["schema_type"])),
			"source_database": discoveryText(row["source_database"]),
		})
	}
	return items, nil
}
