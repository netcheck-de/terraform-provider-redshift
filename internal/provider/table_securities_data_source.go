package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newTableSecuritiesDataSource)

// tableSecurityOutputDescriptions describe the listed values instead of the resource's configuration advice.
var tableSecurityOutputDescriptions = map[string]string{
	"database":                     "Database containing the relation.",
	"schema":                       "Schema of the relation.",
	"relation":                     "Table, view, or materialized view.",
	"row_level_security":           "Whether row-level security is on for the relation.",
	"conjunction_type":             "`AND` or `OR`: how several policies attached for the same user combine.",
	"datashare_row_level_security": "Whether row-level security is on for datashare consumers (`FOR DATASHARES`).",
}

// newTableSecuritiesDataSource lists the row-level security settings svv_rls_relation reports for one database.
func newTableSecuritiesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "table_securities",
		databaseField: "database",
		description:   "Lists the row-level security settings of the relations of one database that `svv_rls_relation` reports. AWS documents the view as listing RLS-protected relations; a relation the view does not report is absent here, while `redshift_table_security` reports the defaults for it (off, `AND`, on for datashares). Superusers see every relation; other users see only their own. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database whose relations are listed; defaults to the provider database."},
			"schema":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only relations of this schema; all schemas when omitted."},
		},
		element: describedOutputs(collectionElement(newTableSecurityResource, "database", "schema", "relation", "row_level_security", "conjunction_type", "datashare_row_level_security"), tableSecurityOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, local, err := discoveryLocalDatabase(ctx, client, filters, "database")
			if err != nil || !local {
				return []map[string]attr.Value{}, err
			}

			rows, err := client.selectRows(ctx, database, listTableSecuritiesQuery(database, objectString(filters, "schema")))
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				settings, err := tableSecuritySettings(row)
				if err != nil {
					return nil, err
				}

				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "schema": types.StringValue(row["relschema"]), "relation": types.StringValue(row["relname"]),
					"row_level_security": settings.RowLevelSecurity, "conjunction_type": settings.ConjunctionType,
					"datashare_row_level_security": settings.DatashareRowLevelSecurity,
				})
			}

			return items, nil
		},
	})
}
