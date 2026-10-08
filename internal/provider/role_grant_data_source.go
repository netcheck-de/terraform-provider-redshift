package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newRoleGrantDataSource checks an explicit role-to-role or role-to-user relationship.
func newRoleGrantDataSource() datasource.DataSource {
	return newCatalogDataSource("role_grant", newRoleGrantResource, nil, true, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := roleGrantModel{Role: attributes["role"].(types.String), ToRole: attributes["to_role"].(types.String), ToUser: attributes["to_user"].(types.String)}
		return (&roleGrantResource{*client}).read(ctx, model)
	})
}
