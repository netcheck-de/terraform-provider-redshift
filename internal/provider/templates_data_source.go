package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newTemplatesDataSource)

// newTemplatesDataSource lists COPY templates of one schema, or of every local schema of a database.
func newTemplatesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "templates",
		description: "Lists COPY templates visible in `SYS_REDSHIFT_TEMPLATE`. Superusers see all templates; regular users see only their own. No schema scan or schema USAGE privilege is required.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Local database to list; the provider database when omitted."},
			"schema":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only visible templates of this schema; all visible templates of the database when omitted."},
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

			rows, err := client.selectRows(ctx, database, listTemplatesQuery(database, objectString(filters, "schema")))
			if err != nil {
				return nil, err
			}

			items := []map[string]attr.Value{}
			for _, row := range rows {
				// Quoted identifiers may contain meaningful leading or trailing whitespace.
				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "schema": types.StringValue(row["schema_name"]),
					"name": types.StringValue(row["template_name"]), "owner": types.StringValue(row["template_owner"]),
				})
			}

			return items, nil
		},
	})
}
