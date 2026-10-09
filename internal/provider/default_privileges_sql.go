package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// readDefaultPrivilegesQuery reads the default privileges one owner grants to one grantee for an object kind.
// An empty schema selects the database-wide defaults, which the catalog reports without a schema name.
func readDefaultPrivilegesQuery(owner, schema, catalogType, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("privilege_type", "admin_option").From("svv_default_privileges").
		Where("owner_name = :owner", sqlclient.Bind("owner", owner)).
		WhereEither(schema != "", "schema_name = :schema", "(schema_name IS NULL OR schema_name = '')", sqlclient.Bind("schema", schema)).
		Where("object_type = :object_type", sqlclient.Bind("object_type", catalogType)).
		Where("grantee_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("grantee_type = LOWER(:kind)", sqlclient.Bind("kind", granteeType))
}

// defaultPrivilegesTarget validates a future-object tuple and renders its checks, catalog read, and
// ALTER DEFAULT PRIVILEGES statements.
func defaultPrivilegesTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	database, owner, schemaName, kind := objectString(data, "database_name"), objectString(data, "owner"), objectString(data, "schema_name"), objectString(data, "object_type")
	var keyword sqlclient.Keyword
	var catalogType string
	allowed := []sqlclient.Keyword{"EXECUTE"}
	switch kind {
	case "TABLES":
		keyword, catalogType = "TABLES", "RELATION"
		allowed = []sqlclient.Keyword{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "TRUNCATE"}
	case "FUNCTIONS":
		keyword, catalogType = "FUNCTIONS", "FUNCTION"
	case "PROCEDURES":
		keyword, catalogType = "PROCEDURES", "PROCEDURE"
	default:
		return privilegeTarget{}, fmt.Errorf("unsupported default object_type %q", kind)
	}
	queries := []sqlclient.Query{localDatabaseQuery(database), privilegeUserQuery(owner)}
	if schemaName != "" {
		queries = append(queries, privilegeSchemaQuery(database, schemaName))
	}
	checks, err := newCatalogChecks(queries...)
	if err != nil {
		return privilegeTarget{}, err
	}
	query, err := newCatalogCheck(readDefaultPrivilegesQuery(owner, schemaName, catalogType, objectString(data, "grantee"), objectString(data, "grantee_type")))
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		database: database, checks: append([]catalogCheck{check}, checks...), query: query, allowed: allowed,
		grant: grantSpec{
			prefix:  sqlclient.Stmt("ALTER DEFAULT PRIVILEGES FOR USER").Ident(owner).OptIdent("IN SCHEMA", schemaName),
			object:  sqlclient.Kw("ON", keyword),
			grantee: grantee,
		},
	}, nil
}
