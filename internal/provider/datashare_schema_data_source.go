package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newDatashareSchemaDataSource observes schema membership and its future-object sharing policy.
func newDatashareSchemaDataSource() datasource.DataSource {
	return newCatalogDataSource("datashare_schema", newDatashareSchemaResource, []string{"include_new"}, true, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareSchemaModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), Schema: attributes["schema"].(types.String), IncludeNew: types.BoolValue(false)}
		found, err := (&datashareSchemaResource{*client}).read(ctx, &model)
		lookupValue(data, "include_new", model.IncludeNew)
		return found, err
	})
}
