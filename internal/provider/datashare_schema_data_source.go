package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareSchemaDataSource)

// newDatashareSchemaDataSource observes schema membership and its future-object sharing policy.
func newDatashareSchemaDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "datashare_schema", factory: newDatashareSchemaResource, computed: []string{"include_new"}, exists: true, identityFields: []string{"datashare", "schema"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareSchemaModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), Schema: attributes["schema"].(types.String), IncludeNew: types.BoolValue(false)}
		found, err := (&datashareSchemaResource{*client}).read(ctx, &model)
		lookupValue(data, "include_new", model.IncludeNew)
		return found, err
	}})
}
