package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newSystemGrantsDataSource)

// newSystemGrantsDataSource lists the system permissions granted directly to roles.
func newSystemGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "system_grants",
		description: "Lists the system permissions granted directly to roles from `SVV_SYSTEM_PRIVILEGES`, one element per role, without taking ownership. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every role; other users see only the identities they have access to or own.",
		filters: map[string]schema.Attribute{
			"role": schema.StringAttribute{Optional: true, MarkdownDescription: "Only the system permissions of this role.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
		element: describedOutputs(collectionElement(newSystemGrantResource, "role", "privileges"), map[string]string{
			"role":       "Role holding the system permissions.",
			"privileges": "System permissions granted directly to the role, such as `CREATE USER` or `ACCESS SYSTEM TABLE`.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listSystemGrantsQuery(objectString(data, "role")))
			if err != nil {
				return nil, err
			}

			groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string { return []string{row["identity_name"]} })

			items := make([]map[string]attr.Value, 0, len(groups))
			for _, group := range groups {
				items = append(items, map[string]attr.Value{"role": types.StringValue(group.key[0]), "privileges": group.fold.privilegeSet()})
			}

			return items, nil
		},
	})
}
