package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newRoleGrantDataSource)

// newRoleGrantDataSource checks an explicit role-to-role or role-to-user relationship and its admin option.
func newRoleGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "role_grant", factory: newRoleGrantResource, exists: true, identityFields: []string{"role", "to_user", "to_role"}, lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := roleGrantModel{Role: attributes["role"].(types.String), ToRole: attributes["to_role"].(types.String), ToUser: attributes["to_user"].(types.String), AdminOption: types.BoolNull()}
		found, err := (&roleGrantResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "admin_option", model.AdminOption)
		}
		return found, err
	}})
}
