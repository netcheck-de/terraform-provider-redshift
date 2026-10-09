package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newDefaultPrivilegesResource)

// newDefaultPrivilegesResource defines creator-specific future object permission tuples.
func newDefaultPrivilegesResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["database_name"] = privilegeString("Local database receiving default privileges.", false)
	attributes["owner"] = privilegeString("User creating the future objects.", false)
	attributes["schema_name"] = privilegeString("Optional schema; omit for database-wide defaults.", true)
	attributes["object_type"] = privilegeString("TABLES, FUNCTIONS, or PROCEDURES.", false, "TABLES", "FUNCTIONS", "PROCEDURES")
	return &privilegeResource{name: "default_privileges", attributes: attributes, fields: []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}, prepare: defaultPrivilegesTarget}
}
