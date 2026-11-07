package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newGroupsDataSource)

// newGroupsDataSource lists every SQL user group with its ID and members.
func newGroupsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "groups",
		description: "Lists every SQL user group from `pg_group`, which every user can read, with its ID and member users.",
		element: describedOutputs(collectionElement(newGroupResource, "name", "group_id", "members"), map[string]string{
			"members": "Names of the users currently in the group; empty when it has none.",
		}),
		list: func(ctx context.Context, client *resourceClient, _ types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listGroupsQuery())
			if err != nil {
				return nil, err
			}

			groups, err := groupsCatalog(rows)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(groups))
			for _, group := range groups {
				items = append(items, map[string]attr.Value{"name": group.Name, "group_id": group.GroupID, "members": group.Members})
			}

			return items, nil
		},
	})
}
