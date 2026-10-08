package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

// newObjectGrantDataSource reads explicit privileges for one local object and SQL grantee.
func newObjectGrantDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newObjectGrantResource)
}
