package provider

import "github.com/hashicorp/terraform-plugin-framework/datasource"

var _ = registerDataSource(newAssumeroleGrantDataSource)

// newAssumeroleGrantDataSource reads explicit IAM role command permissions for one SQL identity.
func newAssumeroleGrantDataSource() datasource.DataSource {
	return newPrivilegeDataSource(newAssumeroleGrantResource)
}
