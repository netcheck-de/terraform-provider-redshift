package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// newDefaultPrivilegesResource defines creator-specific future object permission tuples.
func newDefaultPrivilegesResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["database_name"] = privilegeString("Local database receiving default privileges.", false)
	attributes["owner"] = privilegeString("User creating the future objects.", false)
	attributes["schema_name"] = privilegeString("Optional schema; omit for database-wide defaults.", true)
	attributes["object_type"] = privilegeString("TABLES, FUNCTIONS, or PROCEDURES.", false, "TABLES", "FUNCTIONS", "PROCEDURES")
	return &privilegeResource{name: "default_privileges", attributes: attributes, fields: []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}, prepare: func(data types.Object) (privilegeTarget, error) {
		recipient, check, err := principal(data)
		if err != nil {
			return privilegeTarget{}, err
		}
		database, owner, schemaName, kind := objectString(data, "database_name"), objectString(data, "owner"), objectString(data, "schema_name"), objectString(data, "object_type")
		allowed := []string{"EXECUTE"}
		catalogType := "FUNCTION"
		switch kind {
		case "TABLES":
			catalogType = "RELATION"
			allowed = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "TRUNCATE"}
		case "PROCEDURES":
			catalogType = "PROCEDURE"
		case "FUNCTIONS":
		default:
			return privilegeTarget{}, fmt.Errorf("unsupported default object_type %q", kind)
		}
		checks := []catalogCheck{
			check,
			{"SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local'", map[string]string{"database": database}},
			{"SELECT usename FROM pg_user WHERE usename = :name", map[string]string{"name": owner}},
		}
		prefix := "ALTER DEFAULT PRIVILEGES FOR USER " + sqlclient.Identifier(owner) + " "
		if schemaName != "" {
			prefix += "IN SCHEMA " + sqlclient.Identifier(schemaName) + " "
			checks = append(checks, catalogCheck{"SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema", map[string]string{"database": database, "schema": schemaName}})
		}
		parameters := map[string]string{"owner": owner, "object_type": catalogType, "grantee": objectString(data, "grantee"), "kind": objectString(data, "grantee_type")}
		condition := "(schema_name IS NULL OR schema_name = '')"
		if schemaName != "" {
			condition = "schema_name = :schema"
			parameters["schema"] = schemaName
		}
		return privilegeTarget{
			database: database, checks: checks, prefix: prefix, object: " ON " + kind, recipient: recipient, allowed: allowed,
			query: catalogCheck{"SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND " + condition + " AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind)", parameters},
		}, nil
	}}
}
