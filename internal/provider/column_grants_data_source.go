package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newColumnGrantsDataSource)

// newColumnGrantsDataSource lists explicit column privileges in one database, one element per relation and grantee.
func newColumnGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "column_grants",
		databaseField: "database_name",
		description: "Lists explicit column-level privileges in one local database from `SVV_COLUMN_PRIVILEGES`, one element per table or view and grantee, without taking ownership. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every grant; other users see only the identities they have access to or own. " + discoveryLocalScope,
		filters: grantsGranteeFilters(),
		element: describedOutputs(collectionElement(newColumnGrantResource, "database_name", "schema_name", "object_name", "grantee", "grantee_type", "privileges"), map[string]string{
			"database_name": "Database containing the relation.",
			"schema_name":   "Schema containing the relation.",
			"object_name":   "Table, view, or materialized view.",
			"grantee":       "Receiving identity name; `public` for `PUBLIC`.",
			"grantee_type":  "`USER`, `ROLE`, `GROUP`, or `PUBLIC`.",
			"privileges":    "Map from `SELECT` or `UPDATE` to the set of columns that hold it.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			database, local, err := discoveryLocalDatabase(ctx, client, data, "database_name")
			if err != nil || !local {
				return []map[string]attr.Value{}, err
			}

			query := listColumnGrantsQuery(objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "grantee"), objectString(data, "grantee_type"))

			rows, err := client.selectRows(ctx, database, query)
			if err != nil {
				return nil, err
			}

			return columnGrantsItems(database, rows), nil
		},
	})
}
