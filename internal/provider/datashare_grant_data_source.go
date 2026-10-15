package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareGrantDataSource)

// newDatashareGrantDataSource checks explicit SQL usage granted to a consumer account or namespace.
func newDatashareGrantDataSource() datasource.DataSource {
	source := newCatalogDataSource(catalogSpec{name: "datashare_grant", factory: newDatashareGrantResource, exists: true, identityFields: []string{"datashare", "account_id", "namespace_id", "via_data_catalog"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareGrantModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), AccountID: attributes["account_id"].(types.String), NamespaceID: attributes["namespace_id"].(types.String), ViaDataCatalog: attributes["via_data_catalog"].(types.Bool)}
		return (&datashareGrantResource{*client}).read(ctx, model)
	}}).(*catalogDataSource)
	// The resource computes via_data_catalog only to apply its default, which would make the lookup observe it;
	// it stays a selector, because the catalog cannot report which account form a grant uses.
	observed := source.attributes["via_data_catalog"]
	source.attributes["via_data_catalog"] = schema.BoolAttribute{Optional: true, MarkdownDescription: observed.GetMarkdownDescription()}
	return source
}
