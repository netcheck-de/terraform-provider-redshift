package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaskingPolicyDataSource)

// newMaskingPolicyDataSource reads one masking policy's inputs and catalog expression without owning it.
func newMaskingPolicyDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "masking_policy", factory: newMaskingPolicyResource, computed: []string{"input_columns", "expression"},
		identityFields: []string{"name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			model := maskingPolicyModel{ID: types.StringNull(), Database: data.Attributes()["database"].(types.String), Name: data.Attributes()["name"].(types.String), InputColumns: types.ListNull(maskingPolicyColumnType)}
			text, found, err := (&maskingPolicyResource{*client}).read(ctx, &model)
			if err == nil && found {
				lookupValue(data, "input_columns", model.InputColumns)
				lookupValue(data, "expression", types.StringValue(text))
				lookupValue(data, "definition_fingerprint", types.StringValue(definitionFingerprint(text)))
			}
			return found, err
		},
	})
}
