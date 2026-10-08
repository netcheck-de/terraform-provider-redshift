package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

// newAssumeroleGrantDataSource reads explicit IAM role command permissions for one SQL identity.
func newAssumeroleGrantDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newAssumeroleGrantResource)
}
