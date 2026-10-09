package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newSystemGrantDataSource)

// newSystemGrantDataSource reads explicit system capabilities granted to one SQL role.
func newSystemGrantDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newSystemGrantResource)
}
