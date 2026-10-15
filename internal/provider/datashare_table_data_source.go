package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareTableDataSource)

// newDatashareTableDataSource checks explicit membership of a table or view in a producer share.
func newDatashareTableDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "datashare_table", factory: newDatashareTableResource, exists: true, identityFields: []string{"datashare", "schema", "table"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareTableModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), Schema: attributes["schema"].(types.String), Table: attributes["table"].(types.String)}
		return (&datashareTableResource{*client}).read(ctx, model)
	}})
}
