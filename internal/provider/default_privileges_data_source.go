package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

// newDefaultPrivilegesDataSource reads explicit creator-specific future-object permissions.
func newDefaultPrivilegesDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newDefaultPrivilegesResource)
}
