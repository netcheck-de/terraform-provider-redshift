package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newViewsDataSource)

// newViewsDataSource lists the ordinary and late-binding views of one database, the objects redshift_view manages.
func newViewsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "views",
		databaseField: "database",
		description: "Lists the ordinary and late-binding views of one local database from `pg_views`, without materialized views or system views. " +
			"A database that is not local, such as a datashare consumer database, lists no views.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("views"),
			"schema":   discoveryFilter("Only views in this schema."),
		},
		element: describedOutputs(collectionElement(newViewResource, "database", "schema", "name", "query", "late_binding", "owner", "definition_fingerprint"), viewOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := discoveryDatabase(client, filters)

			views, err := listViewCatalog(ctx, client, database, objectString(filters, "schema"))
			if err != nil {
				return nil, err
			}

			items := []map[string]attr.Value{}

			for _, view := range views {
				if view.materialized {
					continue
				}

				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "schema": types.StringValue(view.schema), "name": types.StringValue(view.name),
					"query": types.StringValue(view.definition), "late_binding": types.BoolValue(view.lateBinding),
					"owner": types.StringValue(view.owner), "definition_fingerprint": types.StringValue(definitionFingerprint(view.definition)),
				})
			}

			return items, nil
		},
	})
}

// viewCatalogListing is one listed view or materialized view with its identity.
type viewCatalogListing struct {
	viewCatalogEntry

	// schema and name identify the relation as the catalog reports them.
	schema, name string
}

// listViewCatalog reads the views and materialized views of a local database from pg_views, which covers only the
// connected database, so the read runs in the listed database. A database that is not local lists nothing.
func listViewCatalog(ctx context.Context, client *resourceClient, database, schemaName string) ([]viewCatalogListing, error) {
	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return nil, err
	}

	rows, err := client.selectRows(ctx, database, listViewsQuery(schemaName))
	if err != nil {
		return nil, err
	}

	views := make([]viewCatalogListing, 0, len(rows))
	for _, row := range rows {
		if row["schemaname"] == "" || row["viewname"] == "" {
			return nil, fmt.Errorf("pg_views returned a view without a schema or name in database %q", database)
		}

		entry, err := viewCatalogEntryOf(row)
		if err != nil {
			return nil, err
		}

		views = append(views, viewCatalogListing{viewCatalogEntry: entry, schema: row["schemaname"], name: row["viewname"]})
	}

	return views, nil
}
