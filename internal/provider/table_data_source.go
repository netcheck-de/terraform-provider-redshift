package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ = registerDataSource(newTableDataSource)

// newTableDataSource reads a table definition with the resource's catalog read, so lookup and resource report the
// same values.
func newTableDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "table", factory: newTableResource,
		computed:       []string{"columns", "primary_key", "unique", "foreign_keys", "distkey", "sortkey"},
		identityFields: []string{"schema", "name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			var selected tableModel
			if diagnostics := data.As(ctx, &selected, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
				return false, fmt.Errorf("read table lookup configuration: %v", diagnostics)
			}
			// Nothing but the selectors may shape the result, so the lookup reports catalog spellings.
			observed := tableNullModel(selected.Database, selected.Schema, selected.Name)
			found, _, err := (&tableResource{*client}).read(ctx, &observed)
			if err != nil || !found {
				return found, err
			}
			value, diagnostics := types.ObjectValueFrom(ctx, data.AttributeTypes(ctx), observed)
			if diagnostics.HasError() {
				return false, fmt.Errorf("convert table lookup: %v", diagnostics)
			}
			*data = value
			return true, nil
		},
	})
}
