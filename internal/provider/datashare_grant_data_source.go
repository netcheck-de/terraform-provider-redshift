package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newDatashareGrantDataSource checks explicit SQL usage granted to a consumer account or namespace.
func newDatashareGrantDataSource() datasource.DataSource {
	return newCatalogDataSource("datashare_grant", newDatashareGrantResource, nil, true, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareGrantModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), AccountID: attributes["account_id"].(types.String), NamespaceID: attributes["namespace_id"].(types.String)}
		return (&datashareGrantResource{*client}).read(ctx, model)
	})
}
