package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newRoleGrantsDataSource)

// newRoleGrantsDataSource lists explicit role grants to users and to roles.
func newRoleGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "role_grants",
		description: "Lists explicit role grants to users from `SVV_USER_GRANTS` and to roles from `SVV_ROLE_GRANTS`; inherited access is not listed. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every grant; other users see only the roles granted to them " +
			"and the role grants of identities they have access to or own.",
		filters: map[string]schema.Attribute{
			"role":    discoveryFilter("Only grants of the role with exactly this name."),
			"to_user": roleGrantsRecipientFilter("Only grants to the user with exactly this name; excludes grants to roles.", "to_role"),
			"to_role": roleGrantsRecipientFilter("Only grants to the role with exactly this name; excludes grants to users.", "to_user"),
		},
		element: describedOutputs(collectionElement(newRoleGrantResource, "role", "to_user", "to_role", "admin_option"), map[string]string{
			"to_user":      "Receiving database user; null for a grant to a role.",
			"to_role":      "Receiving role; null for a grant to a user.",
			"admin_option": "Whether the receiving user holds the role `WITH ADMIN OPTION` and can grant it to other users and roles (`svv_user_grants.admin_option`); always `false` for a grant to a role.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			role, toUser, toRole := objectString(filters, "role"), objectString(filters, "to_user"), objectString(filters, "to_role")
			database := client.database.ValueString()
			items := []map[string]attr.Value{}

			// A recipient filter of one kind excludes every grant to the other kind, so that view is not read.
			// The filters conflict, so a configuration never sets both.
			if toRole == "" {
				rows, err := client.selectRows(ctx, database, listRoleUserGrantsQuery(role, toUser))
				if err != nil {
					return nil, err
				}

				userItems, err := roleUserGrantItems(rows)
				if err != nil {
					return nil, err
				}

				items = append(items, userItems...)
			}

			if toUser == "" {
				rows, err := client.selectRows(ctx, database, listRoleRoleGrantsQuery(role, toRole))
				if err != nil {
					return nil, err
				}

				for _, row := range rows {
					items = append(items, map[string]attr.Value{
						"role": types.StringValue(row["granted_role_name"]), "to_user": types.StringNull(),
						"to_role": types.StringValue(row["role_name"]), "admin_option": types.BoolValue(false),
					})
				}
			}

			return items, nil
		},
	})
}

// roleGrantsRecipientFilter is a recipient filter that conflicts with the other recipient kind, because a grant has
// exactly one recipient.
func roleGrantsRecipientFilter(description, other string) schema.StringAttribute {
	filter := discoveryFilter(description + " Conflicts with `" + other + "`.")
	filter.Validators = append(filter.Validators, stringvalidator.ConflictsWith(path.MatchRoot(other)))

	return filter
}

// roleUserGrantItems folds the per-grantor svv_user_grants rows into one grant per role and user, in order of
// first appearance, with the admin option the lookup reports.
func roleUserGrantItems(rows []sqlclient.Row) ([]map[string]attr.Value, error) {
	type grant struct{ role, user string }

	var order []grant

	grouped := map[grant][]sqlclient.Row{}

	for _, row := range rows {
		key := grant{role: row["role_name"], user: row["user_name"]}
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}

		grouped[key] = append(grouped[key], row)
	}

	items := make([]map[string]attr.Value, 0, len(order))
	for _, key := range order {
		admin, err := roleGrantAdminOption(grouped[key])
		if err != nil {
			return nil, err
		}

		items = append(items, map[string]attr.Value{
			"role": types.StringValue(key.role), "to_user": types.StringValue(key.user),
			"to_role": types.StringNull(), "admin_option": types.BoolValue(admin),
		})
	}

	return items, nil
}
