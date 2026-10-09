package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareGrantDataSource)

// newDatashareGrantDataSource checks explicit SQL usage granted to a consumer account or namespace.
func newDatashareGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "datashare_grant", factory: newDatashareGrantResource, exists: true, identityFields: []string{"datashare", "account_id", "namespace_id"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareGrantModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), AccountID: attributes["account_id"].(types.String), NamespaceID: attributes["namespace_id"].(types.String)}
		return (&datashareGrantResource{*client}).read(ctx, model)
	}})
}
