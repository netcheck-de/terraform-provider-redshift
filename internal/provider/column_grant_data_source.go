package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newColumnGrantDataSource)

// newColumnGrantDataSource reads one grantee's explicit column privileges on one table or view.
func newColumnGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "column_grant", factory: newColumnGrantResource, computed: []string{"privileges"}, identityFields: columnGrantFields, lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := columnGrantModel{
			ID:           types.StringNull(),
			DatabaseName: attributes["database_name"].(types.String), SchemaName: attributes["schema_name"].(types.String),
			ObjectName: attributes["object_name"].(types.String), Grantee: attributes["grantee"].(types.String),
			GranteeType: attributes["grantee_type"].(types.String),
		}
		_, found, err := (&columnGrantResource{*client}).read(ctx, &model, false)
		if err == nil && found {
			lookupValue(data, "privileges", model.Privileges)
		}
		return found, err
	}})
}
