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

var _ = registerDataSource(newDatasharesDataSource)

// newDatasharesDataSource lists the inbound and outbound datashares of the provider's namespace.
func newDatasharesDataSource() datasource.DataSource {
	element := collectionElement(newDatashareResource, "name", "publicly_accessible", "owner", "share_id", "producer_account", "producer_namespace", "created_at")
	element["publicly_accessible"] = datasharePublicObservation
	element["database"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Producer database of an outbound share; null for an inbound share."}
	element["share_type"] = schema.StringAttribute{Computed: true, MarkdownDescription: "`OUTBOUND` for a share this namespace produces, `INBOUND` for one it consumes."}
	element["consumer_database"] = schema.StringAttribute{Computed: true, MarkdownDescription: "Database created from an inbound share; null when none exists."}
	element["managed_by"] = schema.StringAttribute{Computed: true, MarkdownDescription: "AWS service managing the share, such as `ADX`; null for a share managed with SQL."}
	return newCollectionDataSource(collectionSpec{
		name:        "datashares",
		description: "Lists the inbound and outbound datashares visible in the provider's namespace from `SVV_DATASHARES`, without taking ownership.",
		filters: map[string]schema.Attribute{
			"share_type": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.OneOf("INBOUND", "OUTBOUND")},
				MarkdownDescription: "Only shares of this type: `INBOUND` or `OUTBOUND`.",
			},
			"name": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Only shares with exactly this name; an inbound and an outbound share may share a name.",
			},
		},
		element: element,
		list:    datasharesList,
	})
}

// datasharesList reads the matching shares in the administration database; SVV_DATASHARES covers every database
// of the namespace, so no database needs to be selected.
func datasharesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	rows, err := client.selectRows(ctx, client.database.ValueString(), listDatasharesQuery(objectString(filters, "share_type"), objectString(filters, "name")))
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
			"name": types.StringValue(strings.TrimSpace(row["share_name"])), "share_type": datashareText(row["share_type"]),
			"database": datashareText(row["source_database"]), "consumer_database": datashareText(row["consumer_database"]),
			"managed_by": datashareText(row["managed_by"]), "publicly_accessible": public, "owner": metadata.owner,
			"share_id": metadata.shareID, "producer_account": metadata.producerAccount, "producer_namespace": metadata.producerNamespace,
			"created_at": metadata.createdAt,
		})
	}
	return items, nil
}
