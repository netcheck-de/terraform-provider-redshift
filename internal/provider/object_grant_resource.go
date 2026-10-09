package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// newObjectGrantResource defines explicit local object privileges for a SQL grantee.
func newObjectGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["database_name"] = privilegeString("Local database containing the object.", false)
	attributes["schema_name"] = privilegeString("Required for TABLE and SCHEMA; omit for DATABASE.", true)
	attributes["object_name"] = privilegeString("Table or view name; required only for TABLE.", true)
	attributes["object_type"] = privilegeString("TABLE (including views), SCHEMA, or DATABASE.", false, "TABLE", "SCHEMA", "DATABASE")
	return &privilegeResource{name: "object_grant", attributes: attributes, fields: []string{"database_name", "schema_name", "object_name", "object_type", "grantee", "grantee_type"}, prepare: objectGrantTarget}
}
