package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newTemplatesDataSource)

// newTemplatesDataSource lists COPY templates of one schema, or of every local schema of a database.
func newTemplatesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "templates",
		description: "Lists COPY templates with `SHOW TEMPLATES FROM SCHEMA`, for one schema or for every local schema of the database. Listing a schema requires `USAGE` on it.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Local database to list; the provider database when omitted."},
			"schema":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only templates of this schema; every local schema of the database when omitted, which runs one `SHOW TEMPLATES` per schema."},
		},
		element: describedOutputs(collectionElement(newTemplateResource, "database", "schema", "name", "owner"), map[string]string{
			"name":  templateOutputDescriptions["name"],
			"owner": templateOutputDescriptions["owner"],
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := objectString(filters, "database")
			if database == "" {
				database = client.database.ValueString()
			}
			schemas := []string{objectString(filters, "schema")}
			if schemas[0] == "" {
				rows, err := client.selectRows(ctx, database, templateSchemasQuery(database))
				if err != nil {
					return nil, err
				}
				schemas = schemas[:0]
				for _, row := range rows {
					schemas = append(schemas, row["schema_name"])
				}
			}
			items := []map[string]attr.Value{}
			for _, schemaName := range schemas {
				rows, err := client.queryDatabase(ctx, database, showTemplatesStatement("", schemaName, ""), nil)
				if err != nil {
					return nil, err
				}
				// SHOW TEMPLATES documents no order, so the listing sorts by name to keep plans stable.
				slices.SortFunc(rows, func(a, b sqlclient.Row) int { return strings.Compare(a["template_name"], b["template_name"]) })
				for _, row := range rows {
					items = append(items, map[string]attr.Value{
						"database": types.StringValue(database), "schema": types.StringValue(schemaName),
						"name": types.StringValue(row["template_name"]), "owner": types.StringValue(row["template_owner"]),
					})
				}
			}
			return items, nil
		},
	})
}
