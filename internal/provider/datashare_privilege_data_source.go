package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newDatasharePrivilegeDataSource)

// newDatasharePrivilegeDataSource reads the explicit ALTER and SHARE permissions of one identity on a datashare.
func newDatasharePrivilegeDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newDatasharePrivilegeResource)
}
