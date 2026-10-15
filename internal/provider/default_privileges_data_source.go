package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newDefaultPrivilegesDataSource)

// newDefaultPrivilegesDataSource reads explicit creator-specific future-object permissions.
func newDefaultPrivilegesDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newDefaultPrivilegesResource)
}
