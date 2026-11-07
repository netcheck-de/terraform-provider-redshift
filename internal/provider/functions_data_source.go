package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newFunctionsDataSource)

// newFunctionsDataSource lists the SQL user-defined functions redshift_function manages, from pg_proc_info.
func newFunctionsDataSource() datasource.DataSource {
	descriptions := routineCollectionDescriptions("function")
	descriptions["language"] = "Implementation language, always `SQL`."

	return newCollectionDataSource(collectionSpec{
		name:          "functions",
		databaseField: "database",
		description: "Lists scalar SQL user-defined functions (`LANGUAGE SQL`) in one database from `PG_PROC_INFO`, one element per overload, as `redshift_function` manages them. " +
			"Lambda UDFs are listed by `redshift_external_functions`; Python UDFs and the built-in functions in `pg_*` schemas and `information_schema` are excluded. " + discoveryLocalScope,
		filters: routineCollectionFilters("name", "functions"),
		element: routineCollectionElement(newFunctionResource, descriptions,
			"database", "schema", "name", "arguments", "signature", "return_type", "volatility", "language", "owner"),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := routineListingRows(ctx, client, filters, "name", routineListingFunctions, routineListingSQL)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				item, err := functionCollectionItem(database, row)
				if err != nil {
					return nil, err
				}

				item["signature"] = types.StringValue(routineCatalogSignature(row["arguments"]))
				item["language"] = types.StringValue(strings.ToUpper(row["language"]))
				items = append(items, item)
			}

			return items, nil
		},
	})
}
