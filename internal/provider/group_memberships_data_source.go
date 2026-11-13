package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newGroupMembershipsDataSource)

// newGroupMembershipsDataSource lists explicit user-to-group memberships, optionally of one group or one user.
func newGroupMembershipsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "group_memberships",
		description: "Lists explicit user-to-group memberships from `pg_group` and `pg_user`, which every user can read.",
		filters: map[string]schema.Attribute{
			"group": discoveryFilter("Only memberships of the group with exactly this name."),
			"user":  discoveryFilter("Only memberships of the user with exactly this name."),
		},
		element: collectionElement(newGroupMembershipResource, "group", "user"),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			query := listGroupMembershipsQuery(objectString(filters, "group"), objectString(filters, "user"))

			rows, err := client.selectRows(ctx, client.database.ValueString(), query)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				items = append(items, map[string]attr.Value{"group": types.StringValue(row["groname"]), "user": types.StringValue(row["usename"])})
			}

			return items, nil
		},
	})
}
