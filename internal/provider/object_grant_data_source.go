package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newObjectGrantDataSource)

// newObjectGrantDataSource reads explicit privileges for one local object and SQL grantee.
func newObjectGrantDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newObjectGrantResource)
}
