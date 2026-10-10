package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newObjectGrantResource)

// newObjectGrantResource defines explicit local object privileges for a SQL grantee.
func newObjectGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	grantRecipientAttributes(attributes)
	attributes["database_name"] = privilegeString("Local database containing the object. Changing it replaces the grant.", false)
	attributes["schema_name"] = privilegeString("Schema containing the object, or whose objects an `ALL …` snapshot covers; required for every type except `DATABASE`. Changing it replaces the grant.", true)
	attributes["object_name"] = privilegeString("Table, view, function, or procedure name; required for `TABLE`, `FUNCTION`, and `PROCEDURE` only. Changing it replaces the grant.", true)
	attributes["object_type"] = privilegeString("`DATABASE`, `SCHEMA`, `TABLE` (including views), `FUNCTION`, `PROCEDURE`, or a snapshot of a schema's current objects: `ALL TABLES`, `ALL FUNCTIONS`, or `ALL PROCEDURES`. Changing it replaces the grant.", false, objectGrantKinds...)
	attributes["arguments"] = privilegeString("Comma-separated argument types of a `FUNCTION` or `PROCEDURE`, such as `INTEGER, VARCHAR`, which select one overload; omit it for a routine without arguments. Types are canonicalized and lengths dropped, because Redshift identifies overloads by type names only. Changing it replaces the grant.", true)
	attributes["privileges"] = grantPrivilegesAttribute("Exact explicit privilege set; updated in place. An empty set revokes owned privileges. `DATABASE`: `CREATE`, `USAGE`, `TEMPORARY`, `ALTER`. `SCHEMA`: `CREATE`, `USAGE`, `ALTER`, `DROP`. `TABLE`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`, `ALTER`, `TRUNCATE`. `FUNCTION`, `PROCEDURE`, `ALL FUNCTIONS`, and `ALL PROCEDURES`: `EXECUTE`. `ALL TABLES`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`. A snapshot reports only the privileges every current object holds.")
	return &privilegeResource{
		name: "object_grant", attributes: attributes, grantOptions: true, prepare: objectGrantTarget,
		fields: []string{"database_name", "schema_name", "object_name", "object_type", "arguments", "grantee", "grantee_type"},
	}
}
