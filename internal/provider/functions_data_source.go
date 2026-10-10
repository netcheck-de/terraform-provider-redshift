package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newFunctionsDataSource)

// newFunctionsDataSource lists user-defined scalar functions, including Lambda UDFs, from pg_proc_info.
func newFunctionsDataSource() datasource.DataSource {
	element := routineListingElement("name", "function")
	element["return_type"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Result type, as the catalog reports it without length or precision but in uppercase, such as `INTEGER`."}
	element["volatility"] = schema.StringAttribute{Computed: true, MarkdownDescription: "`VOLATILE`, `STABLE`, or `IMMUTABLE`."}
	return newCollectionDataSource(collectionSpec{
		name:        "functions",
		description: "Lists user-defined scalar functions in one database, including SQL, Python, and Lambda (`EXFUNC`) UDFs, without managing them. Built-in functions in `pg_*` schemas and `information_schema` are excluded.",
		filters:     routineListingFilters("name", "functions"),
		element:     element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := routineListingRows(ctx, client, filters, routineListingFunctions, "name")
			if err != nil {
				return nil, err
			}
			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				volatility, err := externalFunctionVolatility(row["volatility"])
				if err != nil {
					return nil, err
				}
				item := routineListingItem(database, "name", row)
				item["return_type"], item["volatility"] = types.StringValue(sqlclient.CatalogType(row["return_type"])), types.StringValue(volatility)
				items = append(items, item)
			}
			return items, nil
		},
	})
}
