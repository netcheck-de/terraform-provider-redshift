package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newObjectGrantResource)

// newObjectGrantResource defines explicit local object privileges for a SQL grantee.
func newObjectGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	grantRecipientAttributes(attributes)
	attributes["database_type"] = privilegeString("Set to `shared` for a consumer database created from a datashare; omit for a local database. Shared grants run from the administration database. Shared routines and snapshots require a SQL superuser to prove complete catalog visibility. Routines have name-wide ACLs, so `FUNCTION` and `PROCEDURE` require arguments to be omitted; producer routine kinds distinguish their identities. Shared procedures use the verified FUNCTION ACL syntax, and `ALL PROCEDURES` enumerates current procedure names instead of issuing unsupported native bulk syntax. `ALL FUNCTIONS` covers all shared routine names, including procedures. Object-level access requires WITH PERMISSIONS.", true, "shared")
	attributes["database_name"] = privilegeString("Database containing the object; local unless `database_type = \"shared\"`.", false)
	attributes["schema_name"] = privilegeString("Schema containing the object, or whose objects an `ALL …` snapshot covers; required for every type except `DATABASE`.", true)
	attributes["object_name"] = privilegeString("Table, view, function, procedure, model, or template name; required for `TABLE`, `FUNCTION`, `PROCEDURE`, `MODEL`, and `TEMPLATE` only.", true)
	attributes["object_type"] = privilegeString("`DATABASE`, `SCHEMA`, `TABLE` (including views), `FUNCTION`, `PROCEDURE`, `MODEL` (a machine learning model, whose prediction or inference function is granted through it), `TEMPLATE` (a COPY template), or a snapshot of a schema's current objects: `ALL TABLES`, `ALL FUNCTIONS`, or `ALL PROCEDURES`.", false, objectGrantKinds...)
	attributes["arguments"] = privilegeString("Comma-separated argument types of a local `FUNCTION` or `PROCEDURE`, such as `INTEGER, VARCHAR`, which select one overload; omit it for a routine without arguments. Shared routine permissions cover every overload of the name, so shared grants must omit this argument. Types are canonicalized and lengths dropped, because Redshift identifies local overloads by type names only.", true)
	attributes["privileges"] = grantPrivilegesAttribute("Exact explicit privilege set; updated in place. An empty set revokes owned privileges. `DATABASE`: `CREATE`, `USAGE`, `TEMPORARY`, `ALTER`. `SCHEMA`: `CREATE`, `USAGE`, `ALTER`, `DROP`. `TABLE`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`, `ALTER`, `TRUNCATE`. `FUNCTION`, `PROCEDURE`, `MODEL`, `ALL FUNCTIONS`, and `ALL PROCEDURES`: `EXECUTE`. `TEMPLATE`: `USAGE`, `ALTER`, `DROP`. `ALL TABLES`: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `DROP`, `REFERENCES`. A snapshot reports only the privileges every current object holds.")
	return &privilegeResource{
		name: "object_grant", attributes: attributes, grantOptions: true, prepare: objectGrantTarget, partialAllowed: objectGrantConfigAllowed,
		fields: []string{"database_name", "database_type", "schema_name", "object_name", "object_type", "arguments", "grantee", "grantee_type"},
	}
}
