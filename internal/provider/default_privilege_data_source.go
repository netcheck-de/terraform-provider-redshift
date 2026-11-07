package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newDefaultPrivilegeDataSource)

// newDefaultPrivilegeDataSource reads explicit creator-specific future-object permissions.
func newDefaultPrivilegeDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newDefaultPrivilegeResource)
}
