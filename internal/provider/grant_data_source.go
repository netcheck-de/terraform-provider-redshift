package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newGrantDataSource)

// newGrantDataSource reads explicit role, user, or datashare privileges for one database/schema scope.
func newGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "grant", factory: newGrantResource, computed: []string{"privileges"}, identityFields: []string{"database_name", "role", "user", "datashare", "scope", "schema_name"}, adjust: grantIdentityFields, lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := grantModel{
			DatabaseName: attributes["database_name"].(types.String), SchemaName: attributes["schema_name"].(types.String),
			Role: attributes["role"].(types.String), User: attributes["user"].(types.String), Datashare: attributes["datashare"].(types.String),
			Scope: attributes["scope"].(types.String),
		}
		_, found, err := (&grantResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "privileges", model.Privileges)
			lookupValue(data, "grant_option_privileges", model.GrantOptionPrivileges)
		}
		return found, err
	}})
}

// grantIdentityFields keeps only the configured recipient in the identity, as the grant resource does, because a
// tuple has exactly one of role, user, or datashare.
func grantIdentityFields(fields map[string]string) {
	if fields["datashare"] != "" || fields["user"] != "" {
		delete(fields, "role")
	}
}
