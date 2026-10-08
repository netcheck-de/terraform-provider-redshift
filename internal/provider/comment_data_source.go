package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// newCommentDataSource reads a local object's annotation without taking ownership of its text.
func newCommentDataSource() datasource.DataSource {
	return newCatalogDataSource("comment", newCommentResource, []string{"text"}, false, func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := commentModel{DatabaseName: attributes["database_name"].(types.String), ObjectType: attributes["object_type"].(types.String), ObjectName: attributes["object_name"].(types.String), SchemaName: attributes["schema_name"].(types.String), ColumnName: attributes["column_name"].(types.String)}
		found, err := (&commentResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "text", model.Text)
		}
		return found, err
	})
}
