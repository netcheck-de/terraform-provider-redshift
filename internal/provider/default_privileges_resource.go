package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newDefaultPrivilegesResource)

// newDefaultPrivilegesResource defines creator-specific future object permission tuples.
func newDefaultPrivilegesResource() resource.Resource {
	attributes := privilegeAttributes()
	grantRecipientAttributes(attributes)
	attributes["privileges"] = grantPrivilegesAttribute("Exact explicit default privilege set; updated in place. `TABLES`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`, `TRUNCATE`. `FUNCTIONS` and `PROCEDURES`: `EXECUTE`. Redshift grants `EXECUTE` on new functions to `PUBLIC` without being asked; the database-wide `PUBLIC` `FUNCTIONS` tuple reports it, an empty set revokes it, and deleting that tuple grants it back.")
	attributes["database_name"] = privilegeString("Local database receiving default privileges. Changing it replaces the grant.", false)
	attributes["owner"] = privilegeString("User whose future objects receive the privileges (`FOR USER`). Omit it to define the defaults of the user the provider connects as, which is what Redshift applies without `FOR USER`; the tuple then follows that connection user. Changing it replaces the grant.", true)
	attributes["schema_name"] = privilegeString("Schema whose future objects receive the privileges (`IN SCHEMA`); omit it for database-wide defaults. Schema defaults add to the database-wide ones and cannot remove them. Changing it replaces the grant.", true)
	attributes["object_type"] = privilegeString("`TABLES` (tables and views), `FUNCTIONS`, or `PROCEDURES`. Changing it replaces the grant.", false, "TABLES", "FUNCTIONS", "PROCEDURES")
	return &privilegeResource{name: "default_privileges", attributes: attributes, fields: []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}, prepare: defaultPrivilegesTarget, grantOptions: true}
}
