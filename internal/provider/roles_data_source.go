package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newRolesDataSource)

// newRolesDataSource lists every role from svv_roles.
func newRolesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "roles",
		description: "Lists every role from `SVV_ROLES`, which every user can read, including the built-in `sys:` roles and the roles of identity providers.",
		element: describedOutputs(collectionElement(newRoleResource, "name", "owner", "external_id", "role_id"), map[string]string{
			"name":        "Role name, including any identity namespace prefix.",
			"owner":       "User that owns the role (`svv_roles.role_owner`).",
			"external_id": "Identifier of the role in a native third-party identity provider (`svv_roles.external_id`); null when unset.",
		}),
		list: func(ctx context.Context, client *resourceClient, _ types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listRolesQuery())
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				var role roleModel
				if err := roleCatalog(&role, row); err != nil {
					return nil, err
				}

				items = append(items, map[string]attr.Value{"name": role.Name, "owner": role.Owner, "external_id": role.ExternalID, "role_id": role.RoleID})
			}

			return items, nil
		},
	})
}
