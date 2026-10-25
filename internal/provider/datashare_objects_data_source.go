package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareObjectsDataSource)

// newDatashareObjectsDataSource lists the objects of the datashares in one local database.
func newDatashareObjectsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "datashare_objects",
		databaseField: "database",
		description:   "Lists current objects in a producer datashare without taking ownership. Includes explicitly added, bulk-added, and automatically included objects; the catalog does not distinguish their origins.",
		filters: map[string]schema.Attribute{
			"database":    schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, MarkdownDescription: "Local producer database; defaults to the provider database."},
			"datashare":   schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, MarkdownDescription: "Only members of this producer datashare; omit for all outbound shares in the selected database."},
			"object_type": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("schema", "table", "view", "late binding view", "materialized view", "function", "stored procedure")}, MarkdownDescription: "Only objects of this catalog type; omit to list all types."},
		},
		element: map[string]schema.Attribute{
			"datashare":   schema.StringAttribute{Computed: true, MarkdownDescription: "Producer datashare name."},
			"object_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Object name exactly as reported by the catalog, including schema qualification. No parsing or overload-signature inference is performed."},
			"object_type": schema.StringAttribute{Computed: true, MarkdownDescription: "Catalog object type."},
			"include_new": schema.BoolAttribute{Computed: true, MarkdownDescription: "Future-object inclusion for a shared schema; null for other object types."},
		},
		list: datashareObjectsList,
	})
}

// datashareObjectsList returns no objects for a missing database instead of failing the lookup.
func datashareObjectsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := objectString(filters, "database")
	if database == "" {
		database = client.database.ValueString()
	}

	exists, err := client.localDatabaseExists(ctx, database)
	if err != nil || !exists {
		return nil, err
	}

	rows, err := client.selectRows(ctx, database, listDatashareObjectsQuery(database, objectString(filters, "datashare"), objectString(filters, "object_type")))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		includeNew := types.BoolNull()

		if row["object_type"] == "schema" && strings.TrimSpace(row["include_new"]) != "" {
			value, err := strconv.ParseBool(strings.TrimSpace(row["include_new"]))
			if err != nil {
				return nil, fmt.Errorf("decode datashare schema include_new: %w", err)
			}

			includeNew = types.BoolValue(value)
		}

		items = append(items, map[string]attr.Value{
			"datashare":   types.StringValue(row["share_name"]),
			"object_name": types.StringValue(row["object_name"]),
			"object_type": types.StringValue(row["object_type"]),
			"include_new": includeNew,
		})
	}

	return items, nil
}
