package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newProcedureDataSource)

// newProcedureDataSource looks up one stored procedure overload by its schema, name, and input arguments.
// nonatomic and configuration stay null because the catalog does not report them.
func newProcedureDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "procedure", factory: newProcedureResource, computed: []string{"body", "nonatomic", "configuration"},
		identityFields: []string{"schema", "name", "signature"}, identityDatabase: "database", adjust: routineLookupIdentity,
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := procedureModel{
				Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String),
				Arguments: attributes["arguments"].(types.List), ID: types.StringNull(), Owner: types.StringNull(), Body: types.StringNull(),
				DefinitionFingerprint: types.StringNull(), Security: types.StringNull(), Nonatomic: types.BoolNull(), Configuration: types.MapNull(types.StringType),
			}
			found, err := (&procedureResource{*client}).read(ctx, &model)
			if err != nil || !found {
				return found, err
			}
			for name, value := range map[string]attr.Value{
				"signature": model.Signature, "body": model.Body, "definition_fingerprint": model.DefinitionFingerprint,
				"security": model.Security, "owner": model.Owner,
			} {
				lookupValue(data, name, value)
			}
			return true, nil
		},
	})
}
