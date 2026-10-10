package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newSystemGrantResource)

// newSystemGrantResource defines role system capabilities and their catalog reconciliation.
func newSystemGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["role"] = privilegeString("Receiving SQL role; Redshift grants system permissions to roles only. Changing it replaces the grant.", false)
	attributes["privileges"] = grantPrivilegesAttribute("Exact set of system permissions held by the role, such as `CREATE USER`, `ACCESS SYSTEM TABLE`, or `IGNORE RLS`; every permission in the GRANT reference's role syntax is accepted. An empty set revokes owned permissions.")
	return &privilegeResource{name: "system_grant", attributes: attributes, fields: []string{"role"}, prepare: systemGrantTarget}
}
