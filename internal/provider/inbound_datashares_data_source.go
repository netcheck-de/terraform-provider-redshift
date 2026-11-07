package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newInboundDatasharesDataSource)

// newInboundDatasharesDataSource lists the datashares other namespaces share with the provider's namespace. No
// resource manages an inbound share: a consumer only creates a database from it.
func newInboundDatasharesDataSource() datasource.DataSource {
	element := describedOutputs(collectionElement(newDatashareResource, "name", "share_id", "producer_account", "producer_namespace", "created_at"), map[string]string{
		"name":               "Datashare name, as named by the producer.",
		"share_id":           "Numeric datashare ID reported by `SVV_DATASHARES`.",
		"producer_account":   "AWS account ID of the producer namespace.",
		"producer_namespace": "Producer namespace UUID; with `producer_account` and `name` it identifies the datashare ARN.",
		"created_at":         "Creation timestamp reported by `SVV_DATASHARES`, as text without a time zone; null when the catalog reports none.",
	})
	element["consumer_database"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Database created from the share in this namespace; null when none exists."}
	element["managed_by"] = schema.StringAttribute{Computed: true, MarkdownDescription: "AWS service managing the share, such as `ADX`; null for a share managed with SQL."}

	return newCollectionDataSource(collectionSpec{
		name:        "inbound_datashares",
		description: "Lists the inbound datashares of the provider's namespace from `SVV_DATASHARES`: shares that other namespaces granted to it. Only superusers, share owners, and users with `ALTER` or `USAGE` on a share see it.",
		filters:     map[string]schema.Attribute{"name": datashareNameFilter},
		element:     element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listDatasharesQuery(false, objectString(filters, "name")))
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				metadata, err := datashareRowMetadata(row)
				if err != nil {
					return nil, err
				}

				items = append(items, map[string]attr.Value{
					"name": types.StringValue(strings.TrimSpace(row["share_name"])), "share_id": metadata.shareID,
					"producer_account": metadata.producerAccount, "producer_namespace": metadata.producerNamespace,
					"created_at": metadata.createdAt, "consumer_database": datashareText(row["consumer_database"]),
					"managed_by": datashareText(row["managed_by"]),
				})
			}

			return items, nil
		},
	})
}
