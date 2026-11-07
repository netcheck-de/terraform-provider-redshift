package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newDatashareTablesDataSource)

// datashareRelationRows are the svv_datashare_objects types the table listing reads: shared schemas, which resolve
// the unquoted schema.table member names, and relations.
const datashareRelationRows sqlclient.Keyword = "object_type IN ('schema', 'schemas', 'table', 'view', 'late binding view', 'materialized view')"

// datashareRelationParts splits an unquoted schema.table member name. The longest shared schema of the same share
// that prefixes the name wins, so a schema name containing a period still resolves; without one, the name is split
// at its first period.
func datashareRelationParts(name string, schemas []string) (string, string) {
	best := ""

	for _, schema := range schemas {
		if len(schema) > len(best) && strings.HasPrefix(name, schema+".") {
			best = schema
		}
	}

	if best != "" {
		return best, strings.TrimPrefix(name, best+".")
	}

	schema, table, _ := strings.Cut(name, ".")

	return schema, table
}

// newDatashareTablesDataSource lists the tables and views of outbound datashares in one producer database.
func newDatashareTablesDataSource() datasource.DataSource {
	filters := datashareObjectFilters()
	filters["schema"] = discoveryFilter("Only relations of this schema; all schemas when omitted.")

	return newCollectionDataSource(collectionSpec{
		name:          "datashare_tables",
		databaseField: "database",
		description:   "Lists the tables, views, late-binding views, and materialized views of the outbound datashares in one producer database from `SVV_DATASHARE_OBJECTS`, without taking ownership. Includes relations added explicitly, in bulk, or by `INCLUDENEW`, which the catalog does not distinguish. " + datashareListVisibility,
		filters:       filters,
		element: describedOutputs(collectionElement(newDatashareTableResource, "database", "datashare", "schema", "table"), map[string]string{
			"schema": "Schema containing the shared relation.",
			"table":  "Shared table or view.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := listDatashareObjects(ctx, client, filters, datashareRelationRows)
			if err != nil {
				return nil, err
			}

			schemas := map[string][]string{}

			for _, row := range rows {
				if datashareListedSchema(row) {
					share := strings.TrimSpace(row["share_name"])
					schemas[share] = append(schemas[share], row["object_name"])
				}
			}

			selected := objectString(filters, "schema")
			items := []map[string]attr.Value{}

			for _, row := range rows {
				if datashareListedSchema(row) {
					continue
				}

				share := strings.TrimSpace(row["share_name"])

				schema, table := datashareRelationParts(row["object_name"], schemas[share])
				if selected != "" && schema != selected {
					continue
				}

				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "datashare": types.StringValue(share),
					"schema": types.StringValue(schema), "table": types.StringValue(table),
				})
			}

			return items, nil
		},
	})
}
