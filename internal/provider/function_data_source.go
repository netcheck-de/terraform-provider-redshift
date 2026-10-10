package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newFunctionDataSource)

// routineLookupIdentity renames the observed signature to the identity's arguments key, so a lookup's ID equals the
// paired resource's, including "" for a routine without input arguments.
func routineLookupIdentity(fields map[string]string) {
	fields[routineIdentityArguments] = fields["signature"]
	delete(fields, "signature")
}

// newFunctionDataSource looks up one SQL UDF overload by its schema, name, and input types.
func newFunctionDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "function", factory: newFunctionResource, computed: []string{"return_type", "body"},
		identityFields: []string{"schema", "name", "signature"}, identityDatabase: "database", adjust: routineLookupIdentity,
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := functionModel{
				Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String),
				Arguments: attributes["arguments"].(types.List), ID: types.StringNull(), Owner: types.StringNull(),
				ReturnType: types.StringNull(), Body: types.StringNull(), DefinitionFingerprint: types.StringNull(),
			}
			found, err := (&functionResource{*client}).read(ctx, &model)
			if err != nil || !found {
				return found, err
			}
			for name, value := range map[string]types.String{
				"signature": model.Signature, "return_type": model.ReturnType, "volatility": model.Volatility, "language": model.Language,
				"body": model.Body, "definition_fingerprint": model.DefinitionFingerprint, "owner": model.Owner,
			} {
				lookupValue(data, name, value)
			}
			return true, nil
		},
	})
}
