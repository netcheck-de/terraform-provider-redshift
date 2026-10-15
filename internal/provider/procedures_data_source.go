package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newProceduresDataSource)

// newProceduresDataSource lists stored procedures from pg_proc_info.
func newProceduresDataSource() datasource.DataSource {
	element := routineListingElement("name", "procedure")
	element["security"] = schema.StringAttribute{Computed: true, MarkdownDescription: "`DEFINER` when the procedure runs with its owner's privileges, otherwise `INVOKER`."}
	return newCollectionDataSource(collectionSpec{
		name:        "procedures",
		description: "Lists stored procedures in one database without managing them. `arguments` holds the `IN` and `INOUT` types that identify each overload; `redshift_routine_parameters` reports every parameter with its mode.",
		filters:     routineListingFilters("name", "procedures"),
		element:     element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := routineListingRows(ctx, client, filters, routineListingProcedures, "name")
			if err != nil {
				return nil, err
			}
			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				definer, err := strconv.ParseBool(row["security_definer"])
				if err != nil {
					return nil, err
				}
				security := "INVOKER"
				if definer {
					security = "DEFINER"
				}
				item := routineListingItem(database, "name", row)
				item["security"] = types.StringValue(security)
				items = append(items, item)
			}
			return items, nil
		},
	})
}
