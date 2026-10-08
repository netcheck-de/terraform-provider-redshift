package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// newObjectGrantResource defines explicit local object privileges for a SQL grantee.
func newObjectGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["database_name"] = privilegeString("Local database containing the object.", false)
	attributes["schema_name"] = privilegeString("Required for TABLE and SCHEMA; omit for DATABASE.", true)
	attributes["object_name"] = privilegeString("Table or view name; required only for TABLE.", true)
	attributes["object_type"] = privilegeString("TABLE (including views), SCHEMA, or DATABASE.", false, "TABLE", "SCHEMA", "DATABASE")
	return &privilegeResource{name: "object_grant", attributes: attributes, fields: []string{"database_name", "schema_name", "object_name", "object_type", "grantee", "grantee_type"}, prepare: func(data types.Object) (privilegeTarget, error) {
		recipient, check, err := principal(data)
		if err != nil {
			return privilegeTarget{}, err
		}
		database, schemaName, name, kind := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "object_type")
		checks := []catalogCheck{check, {"SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local'", map[string]string{"database": database}}}
		object := sqlclient.Identifier(database)
		allowed := []string{"CREATE", "USAGE", "TEMPORARY", "ALTER"}
		switch kind {
		case "DATABASE":
			if schemaName != "" || name != "" {
				return privilegeTarget{}, fmt.Errorf("DATABASE does not accept schema_name or object_name")
			}
		case "SCHEMA", "TABLE":
			if schemaName == "" {
				return privilegeTarget{}, fmt.Errorf("schema_name is required for %s", kind)
			}
			object += "." + sqlclient.Identifier(schemaName)
			checks = append(checks, catalogCheck{"SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema", map[string]string{"database": database, "schema": schemaName}})
			allowed = []string{"CREATE", "USAGE", "ALTER", "DROP"}
			if kind == "TABLE" {
				if name == "" {
					return privilegeTarget{}, fmt.Errorf("object_name is required for TABLE")
				}
				object += "." + sqlclient.Identifier(name)
				checks = append(checks, catalogCheck{"SELECT table_name FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema AND table_name = :name", map[string]string{"database": database, "schema": schemaName, "name": name}})
				allowed = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "ALTER", "TRUNCATE"}
			} else if name != "" {
				return privilegeTarget{}, fmt.Errorf("SCHEMA does not accept object_name")
			}
		default:
			return privilegeTarget{}, fmt.Errorf("unsupported object_type %q", kind)
		}
		return privilegeTarget{
			database: database, checks: checks, object: " ON " + kind + " " + object, recipient: recipient, allowed: allowed,
			query: catalogCheck{sql: "SHOW GRANTS ON " + kind + " " + object},
			filter: func(row sqlclient.Row) bool {
				return row["identity_name"] == objectString(data, "grantee") && strings.EqualFold(row["identity_type"], objectString(data, "grantee_type")) && row["privilege_scope"] == kind
			},
		}, nil
	}}
}
