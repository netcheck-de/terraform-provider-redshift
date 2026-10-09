package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newGrantDataSource)

// newGrantDataSource reads explicit role privileges for one database/schema scope.
func newGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "grant", factory: newGrantResource, computed: []string{"privileges"}, identityFields: []string{"database_name", "role", "datashare", "scope", "schema_name"}, adjust: grantIdentityFields, lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := grantModel{DatabaseName: attributes["database_name"].(types.String), SchemaName: attributes["schema_name"].(types.String), Role: attributes["role"].(types.String), Datashare: attributes["datashare"].(types.String), Scope: attributes["scope"].(types.String)}
		_, found, err := (&grantResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "privileges", model.Privileges)
		}
		return found, err
	}})
}

// grantIdentityFields drops the role from a datashare recipient's identity, as the grant resource does,
// because a datashare tuple has no role.
func grantIdentityFields(fields map[string]string) {
	if fields["datashare"] != "" {
		delete(fields, "role")
	}
}
