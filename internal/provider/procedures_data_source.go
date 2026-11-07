package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newProceduresDataSource)

// newProceduresDataSource lists the stored procedures redshift_procedure manages, from pg_proc_info.
func newProceduresDataSource() datasource.DataSource {
	descriptions := routineCollectionDescriptions("procedure")
	descriptions["signature"] = "`IN` and `INOUT` argument types as one comma-separated string, such as `INTEGER, CHARACTER VARYING`, as `ALTER`, `DROP`, and `GRANT` identify the overload; empty without input arguments."
	descriptions["security"] = "`DEFINER` when the procedure runs with its owner's privileges, otherwise `INVOKER`."

	return newCollectionDataSource(collectionSpec{
		name:          "procedures",
		databaseField: "database",
		description: "Lists stored procedures in one database from `PG_PROC_INFO`, one element per overload, as `redshift_procedure` manages them. " +
			"`redshift_routine_parameters` reports every argument with its name and mode, and `redshift_procedure` the body.",
		filters: routineCollectionFilters("procedures"),
		element: routineCollectionElement(newProcedureResource, descriptions, "database", "schema", "name", "signature", "security", "owner"),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := routineListingRows(ctx, client, filters, "name", routineListingProcedures)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				definer, err := strconv.ParseBool(row["security_definer"])
				if err != nil {
					return nil, fmt.Errorf("procedure %s.%s has an invalid security mode: %w", row["schema_name"], row["routine_name"], err)
				}

				security := "INVOKER"
				if definer {
					security = "DEFINER"
				}

				item := routineCollectionItem(database, row)
				item["signature"] = types.StringValue(routineCatalogSignature(row["arguments"]))
				item["security"] = types.StringValue(security)
				items = append(items, item)
			}

			return items, nil
		},
	})
}
