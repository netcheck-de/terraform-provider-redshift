package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newColumnGrantsDataSource)

// newColumnGrantsDataSource lists explicit column privileges in one database.
func newColumnGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "column_grants",
		description: "Lists explicit column-level privileges in one local database from `SVV_COLUMN_PRIVILEGES`, one element per column, privilege, and grantee, without taking ownership.",
		filters:     grantsGranteeFilters(),
		element: map[string]schema.Attribute{
			"database_name": grantsString("Database containing the relation."),
			"schema_name":   grantsString("Schema containing the relation."),
			"object_name":   grantsString("Table or view name."),
			"column_name":   grantsString("Granted column."),
			"privilege":     grantsString("`SELECT` or `UPDATE`."),
			"grantee":       grantsString("Receiving identity name; `public` for `PUBLIC`."),
			"grantee_type":  grantsString("`USER`, `ROLE`, `GROUP`, or `PUBLIC`."),
		},
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			filters := grantsFiltersFrom(data, client.database.ValueString())
			rows, err := client.selectRows(ctx, filters.database, readColumnGrantsQuery(filters))
			if err != nil {
				return nil, err
			}
			return columnGrantsItems(filters.database, rows), nil
		},
	})
}
