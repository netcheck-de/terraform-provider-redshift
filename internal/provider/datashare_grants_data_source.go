package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareGrantsDataSource)

// newDatashareGrantsDataSource lists the consumers granted usage on the namespace's outbound datashares.
func newDatashareGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "datashare_grants",
		description: "Lists the consumer accounts and namespaces granted `USAGE` on the outbound datashares of every producer database of the provider's namespace from `SVV_DATASHARE_CONSUMERS`, without taking ownership. `SVV_DATASHARE_CONSUMERS` reports an account granted `VIA DATA CATALOG` like a plain account grant, so the form is not listed. Superusers, share owners, and users with `ALTER` or `USAGE` on a share see its consumers; other users see none.",
		filters:     map[string]schema.Attribute{"datashare": discoveryFilter("Only consumers of this outbound datashare; consumers of all visible shares when omitted.")},
		element: describedOutputs(collectionElement(newDatashareGrantResource, "database", "datashare", "account_id", "namespace_id"), map[string]string{
			"account_id":   "Consumer AWS account ID of an account grant; null for a namespace grant.",
			"namespace_id": "Consumer namespace UUID of a namespace grant; null for an account grant.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listDatashareGrantsQuery(objectString(filters, "datashare")))
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				// An account grant has no namespace; a namespace grant also reports its account, which the resource
				// does not configure.
				account, namespace := datashareText(row["consumer_account"]), datashareText(row["consumer_namespace"])
				if !namespace.IsNull() {
					account = types.StringNull()
				}

				items = append(items, map[string]attr.Value{
					"database": datashareText(row["source_database"]), "datashare": types.StringValue(strings.TrimSpace(row["share_name"])),
					"account_id": account, "namespace_id": namespace,
				})
			}

			return items, nil
		},
	})
}
