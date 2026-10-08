package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newGrantDataSource reads explicit role privileges for one database/schema scope.
func newGrantDataSource() datasource.DataSource {
	return newCatalogDataSource("grant", newGrantResource, []string{"privileges"}, false, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := grantModel{DatabaseName: attributes["database_name"].(types.String), SchemaName: attributes["schema_name"].(types.String), Role: attributes["role"].(types.String), Datashare: attributes["datashare"].(types.String), Scope: attributes["scope"].(types.String)}
		_, found, err := (&grantResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "privileges", model.Privileges)
		}
		return found, err
	})
}
