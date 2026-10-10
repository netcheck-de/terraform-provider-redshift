package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaskingPolicyDataSource)

// newMaskingPolicyDataSource reads one masking policy's inputs and catalog expression without owning it.
func newMaskingPolicyDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "masking_policy", factory: newMaskingPolicyResource, computed: []string{"expression"},
		identityFields: []string{"name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			model := maskingPolicyModel{ID: types.StringNull(), Database: data.Attributes()["database"].(types.String), Name: data.Attributes()["name"].(types.String), InputColumn: types.ListNull(maskingPolicyColumnType)}
			text, found, err := (&maskingPolicyResource{*client}).read(ctx, &model)
			if err == nil && found {
				lookupValue(data, "input_column", model.InputColumn)
				lookupValue(data, "expression", types.StringValue(text))
				lookupValue(data, "definition_fingerprint", types.StringValue(definitionFingerprint(text)))
			}
			return found, err
		},
	}), maskingPolicyOutputDescriptions)
}

// maskingPolicyOutputDescriptions describes the catalog spelling of the inputs, which both the lookup and the listing
// report, instead of the resource's apply-time rules for configured spellings.
var maskingPolicyOutputDescriptions = map[string]string{
	"input_column":      "Ordered input columns of the `WITH` clause that the expression reads.",
	"input_column.type": "Data type as the catalog reports it, such as `character varying(256)` for a configured `VARCHAR(256)`.",
}
