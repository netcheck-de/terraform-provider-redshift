package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// objectGrantTableQuery confirms that a table or view exists.
func objectGrantTableQuery(database, schema, name string) sqlclient.Query {
	return sqlclient.Select("table_name").From("svv_all_tables").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).
		Where("table_name = :name", sqlclient.Bind("name", name))
}

// objectGrantTarget validates a database, schema, or table tuple and renders its checks, SHOW GRANTS read, and grants.
func objectGrantTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	database, schemaName, name, kind := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "object_type")
	queries := []sqlclient.Query{localDatabaseQuery(database)}
	parts := []string{database}
	var keyword sqlclient.Keyword
	allowed := []sqlclient.Keyword{"CREATE", "USAGE", "TEMPORARY", "ALTER"}
	switch kind {
	case "DATABASE":
		if schemaName != "" || name != "" {
			return privilegeTarget{}, fmt.Errorf("DATABASE does not accept schema_name or object_name")
		}
		keyword = "DATABASE"
	case "SCHEMA", "TABLE":
		if schemaName == "" {
			return privilegeTarget{}, fmt.Errorf("schema_name is required for %s", kind)
		}
		parts = append(parts, schemaName)
		queries = append(queries, privilegeSchemaQuery(database, schemaName))
		keyword = "SCHEMA"
		allowed = []sqlclient.Keyword{"CREATE", "USAGE", "ALTER", "DROP"}
		if kind == "TABLE" {
			if name == "" {
				return privilegeTarget{}, fmt.Errorf("object_name is required for TABLE")
			}
			parts = append(parts, name)
			queries = append(queries, objectGrantTableQuery(database, schemaName, name))
			keyword = "TABLE"
			allowed = []sqlclient.Keyword{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "ALTER", "TRUNCATE"}
		} else if name != "" {
			return privilegeTarget{}, fmt.Errorf("SCHEMA does not accept object_name")
		}
	default:
		return privilegeTarget{}, fmt.Errorf("unsupported object_type %q", kind)
	}
	checks, err := newCatalogChecks(queries...)
	if err != nil {
		return privilegeTarget{}, err
	}
	object := sqlclient.Kw("ON", keyword).Qualified(parts...)
	return privilegeTarget{
		database: database, checks: append([]catalogCheck{check}, checks...), allowed: allowed,
		grant: grantSpec{object: object, grantee: grantee},
		query: catalogCheck{sql: sqlclient.Stmt("SHOW GRANTS").Append(object).String()},
		filter: func(row sqlclient.Row) bool {
			return row["identity_name"] == objectString(data, "grantee") && strings.EqualFold(row["identity_type"], objectString(data, "grantee_type")) && row["privilege_scope"] == kind
		},
	}, nil
}
