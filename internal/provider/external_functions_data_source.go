package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newExternalFunctionsDataSource)

// newExternalFunctionsDataSource lists the Lambda UDFs redshift_external_function manages, from pg_proc_info.
func newExternalFunctionsDataSource() datasource.DataSource {
	descriptions := routineCollectionDescriptions("function")
	descriptions["volatility"] = "`VOLATILE` or `STABLE`."

	return newCollectionDataSource(collectionSpec{
		name:          "external_functions",
		databaseField: "database",
		description: "Lists Lambda user-defined functions (`LANGUAGE EXFUNC`) in one database from `PG_PROC_INFO`, one element per overload, as `redshift_external_function` manages them. " +
			"No catalog reports a function's Lambda function, IAM role, retry timeout, or batch limits, so the elements carry only its signature, return type, volatility, and owner. " + discoveryLocalScope,
		filters: routineCollectionFilters("name", "functions"),
		element: routineCollectionElement(newExternalFunctionResource, descriptions,
			"database", "schema", "name", "arguments", "return_type", "volatility", "owner"),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := routineListingRows(ctx, client, filters, "name", routineListingFunctions, routineListingLambda)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				item, err := functionCollectionItem(database, row)
				if err != nil {
					return nil, err
				}

				items = append(items, item)
			}

			return items, nil
		},
	})
}
