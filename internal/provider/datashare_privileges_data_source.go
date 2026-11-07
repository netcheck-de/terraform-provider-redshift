package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatasharePrivilegesDataSource)

// newDatasharePrivilegesDataSource lists the explicit ALTER and SHARE permissions on the namespace's outbound
// datashares, one element per share and grantee.
func newDatasharePrivilegesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "datashare_privileges",
		description: "Lists the explicit `ALTER` and `SHARE` permissions on the outbound datashares of the provider's namespace from `SVV_DATASHARE_PRIVILEGES`, one element per share and grantee, without taking ownership. Superusers and users with the `ACCESS SYSTEM TABLE` permission see every grant; other users see only identities they can access or own.",
		filters: map[string]schema.Attribute{
			"datashare_name": discoveryFilter("Only permissions on this outbound datashare; all visible shares when omitted."),
			"grantee":        discoveryFilter("Only permissions of this user, role, or group; `public` selects `PUBLIC`. All grantees when omitted."),
			"grantee_type":   discoveryGranteeTypeFilter(),
		},
		element: describedOutputs(collectionElement(newDatasharePrivilegeResource, "database_name", "datashare_name", "grantee", "grantee_type", "privileges"), map[string]string{
			"database_name": "Producer database owning the datashare.",
			"privileges":    "Explicit permissions of the grantee on the share: `ALTER`, `SHARE`, or both.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listDatasharePrivilegesQuery(objectString(filters, "datashare_name"), objectString(filters, "grantee")))
			if err != nil {
				return nil, err
			}

			type key struct{ database, share, granteeType, grantee string }

			selectedType := objectString(filters, "grantee_type")

			var order []key

			privileges := map[key][]string{}

			for _, row := range rows {
				granteeType := strings.ToUpper(strings.TrimSpace(row["identity_type"]))
				if selectedType != "" && granteeType != selectedType {
					continue
				}

				grantee := row["identity_name"]
				if granteeType == "PUBLIC" {
					// The view documents no name for PUBLIC; the resource addresses it as public.
					grantee = "public"
				}

				item := key{strings.TrimSpace(row["database_name"]), strings.TrimSpace(row["datashare_name"]), granteeType, grantee}
				if _, seen := privileges[item]; !seen {
					order = append(order, item)
				}

				if name := normalizePrivilege(strings.TrimSpace(row["privilege_type"])); !slices.Contains(privileges[item], name) {
					privileges[item] = append(privileges[item], name)
				}
			}

			items := make([]map[string]attr.Value, 0, len(order))
			for _, item := range order {
				values := make([]attr.Value, 0, len(privileges[item]))
				for _, name := range privileges[item] {
					values = append(values, types.StringValue(name))
				}

				items = append(items, map[string]attr.Value{
					"database_name": types.StringValue(item.database), "datashare_name": types.StringValue(item.share),
					"grantee": types.StringValue(item.grantee), "grantee_type": types.StringValue(item.granteeType),
					"privileges": types.SetValueMust(types.StringType, values),
				})
			}

			return items, nil
		},
	})
}
