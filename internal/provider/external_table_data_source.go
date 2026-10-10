package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ = registerDataSource(newExternalTableDataSource)

// newExternalTableDataSource reads an external table's definition, including tables this provider did not create.
// The column descriptions are replaced because the resource's explain how changes plan, which a lookup never does.
func newExternalTableDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "external_table", factory: newExternalTableResource,
		computed: []string{
			"field_delimiter", "line_delimiter", "serde", "serde_properties",
			"stored_as", "input_format", "output_format", "location", "table_properties",
		},
		identityFields: []string{"schema", "name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			var model externalTableModel
			if diagnostics := data.As(ctx, &model, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
				return false, externalTableDiagnosticsError(diagnostics)
			}
			model.ID = types.StringNull()
			catalog, err := (&externalTableResource{*client}).fetch(ctx, model)
			if err != nil || catalog == nil {
				return false, err
			}
			observed := observeExternalTable(catalog, model, externalTableLookup)
			value, diagnostics := types.ObjectValueFrom(ctx, data.AttributeTypes(ctx), observed)
			if diagnostics.HasError() {
				return false, externalTableDiagnosticsError(diagnostics)
			}
			*data = value
			return true, nil
		},
	}), map[string]string{
		"column":        "Data columns in catalog order.",
		"partition_key": "`PARTITIONED BY` columns in key order; null for an unpartitioned table.",
	})
}

// externalTableDiagnosticsError turns conversion diagnostics into the error a catalog lookup returns.
func externalTableDiagnosticsError(diagnostics diag.Diagnostics) error {
	first := diagnostics.Errors()[0]
	return fmt.Errorf("%s: %s", first.Summary(), first.Detail())
}
