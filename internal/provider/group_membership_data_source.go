package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newGroupMembershipDataSource checks one user's explicit group membership without managing it.
func newGroupMembershipDataSource() datasource.DataSource {
	return newCatalogDataSource("group_membership", newGroupMembershipResource, nil, true, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		model := groupMembershipModel{Group: data.Attributes()["group"].(types.String), User: data.Attributes()["user"].(types.String)}
		return (&groupMembershipResource{*client}).read(ctx, model)
	})
}
