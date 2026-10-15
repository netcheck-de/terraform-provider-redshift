package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDebugGrantDataSource)

// newDebugGrantDataSource checks whether a grantee holds DEBUG on a database, as SHOW GRANTS reports it.
func newDebugGrantDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{
		name: "debug_grant", factory: newDebugGrantResource, exists: true,
		identityFields: []string{"grantee", "grantee_type"}, identityDatabase: "database", adjust: debugGrantCanonical,
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			return (&debugGrantResource{*client}).read(ctx, debugGrantLookupModel(*data))
		},
	})
}
