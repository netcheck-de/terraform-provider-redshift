package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaterializedViewsDataSource)

// newMaterializedViewsDataSource lists the materialized views of one database, the objects
// redshift_materialized_view manages.
func newMaterializedViewsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "materialized_views",
		databaseField: "database",
		description: "Lists the materialized views of one local database from `pg_views`, with automatic refresh settings from `SVV_MV_INFO`. " +
			"`SVV_MV_INFO` shows a provider identity that is not a superuser only the materialized views it owns, so `auto_refresh` is null for the others. " +
			discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("materialized views"),
			"schema":   discoveryFilter("Only materialized views in this schema."),
		},
		element: describedOutputs(
			collectionElement(newMaterializedViewResource, "database", "schema", "name", "query", "auto_refresh", "owner", "definition_fingerprint"),
			map[string]string{
				"query":                  materializedViewOutputDescriptions["query"],
				"auto_refresh":           materializedViewOutputDescriptions["auto_refresh"],
				"owner":                  materializedViewOutputDescriptions["owner"],
				"definition_fingerprint": materializedViewOutputDescriptions["definition_fingerprint"],
			},
		),
		list: materializedViewsList,
	})
}

// materializedViewsList reads the materialized views from pg_views and their refresh settings with one SVV_MV_INFO
// read, both in the listed database, because each covers only the connected database.
func materializedViewsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, schemaName := discoveryDatabase(client, filters), objectString(filters, "schema")

	views, err := listViewCatalog(ctx, client, database, schemaName)
	if err != nil {
		return nil, err
	}

	var materialized []viewCatalogListing

	for _, view := range views {
		if view.materialized {
			materialized = append(materialized, view)
		}
	}

	items := make([]map[string]attr.Value, 0, len(materialized))
	if len(materialized) == 0 {
		return items, nil
	}

	rows, err := client.selectRows(ctx, database, listMaterializedViewRefreshQuery(database, schemaName))
	if err != nil {
		return nil, err
	}

	refresh := map[[2]string]bool{}

	for _, row := range rows {
		key := [2]string{row["schema_name"], row["name"]}
		if _, ok := refresh[key]; ok {
			return nil, fmt.Errorf("materialized view %s.%s has ambiguous SVV_MV_INFO rows", key[0], key[1])
		}

		if refresh[key], err = materializedViewFlag(row["autorefresh"]); err != nil {
			return nil, err
		}
	}

	for _, view := range materialized {
		// pg_views decides existence; SVV_MV_INFO hides materialized views of other owners from regular users.
		autoRefresh := types.BoolNull()
		if value, ok := refresh[[2]string{view.schema, view.name}]; ok {
			autoRefresh = types.BoolValue(value)
		}

		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database), "schema": types.StringValue(view.schema), "name": types.StringValue(view.name),
			"query": types.StringValue(view.definition), "auto_refresh": autoRefresh,
			"owner": types.StringValue(view.owner), "definition_fingerprint": types.StringValue(definitionFingerprint(view.definition)),
		})
	}

	return items, nil
}
