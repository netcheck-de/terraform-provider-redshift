package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatasharesDataSource)

// datashareNameFilter narrows a share listing to one name.
var datashareNameFilter = discoveryFilter("Only the share with exactly this name; all visible shares when omitted.")

// newDatasharesDataSource lists the outbound datashares of the provider's namespace.
func newDatasharesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "datashares",
		description: "Lists the outbound datashares of the provider's namespace from `SVV_DATASHARES`, across all of its producer databases, without taking ownership. Only superusers, share owners, and users with `ALTER` or `USAGE` on a share see it.",
		filters:     map[string]schema.Attribute{"name": datashareNameFilter},
		element: describedOutputs(collectionElement(newDatashareResource, "database", "name", "publicly_accessible", "owner", "share_id", "producer_account", "producer_namespace", "created_at"), map[string]string{
			"database":            "Producer database owning the datashare.",
			"publicly_accessible": datasharePublicObservation.MarkdownDescription,
			"owner":               "Database user owning the datashare.",
		}),
		list: datasharesList,
	})
}

// datasharesList reads the matching outbound shares in the administration database; SVV_DATASHARES covers every
// database of the namespace, so no database needs to be selected.
func datasharesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	rows, err := client.selectRows(ctx, client.database.ValueString(), listDatasharesQuery(true, objectString(filters, "name")))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		metadata, err := datashareRowMetadata(row)
		if err != nil {
			return nil, err
		}

		public := types.BoolNull()

		if text := strings.TrimSpace(row["is_publicaccessible"]); text != "" {
			value, err := strconv.ParseBool(text)
			if err != nil {
				return nil, fmt.Errorf("decode datashare public accessibility: %w", err)
			}

			public = types.BoolValue(value)
		}

		items = append(items, map[string]attr.Value{
			"database": datashareText(row["source_database"]), "name": types.StringValue(strings.TrimSpace(row["share_name"])),
			"publicly_accessible": public, "owner": metadata.owner, "share_id": metadata.shareID,
			"producer_account": metadata.producerAccount, "producer_namespace": metadata.producerNamespace, "created_at": metadata.createdAt,
		})
	}

	return items, nil
}
