package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// defaultPrivilegesCurrentUser selects the connecting user's defaults, which ALTER DEFAULT PRIVILEGES changes
// when FOR USER is omitted.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DEFAULT_PRIVILEGES.html
const defaultPrivilegesCurrentUser = "owner_name = current_user"

// defaultPrivilegesImplicitPublic reads PUBLIC's database-wide EXECUTE default on new functions. Redshift grants it
// without a PG_DEFAULT_ACL row, so the catalog has nothing to show until the first ALTER DEFAULT PRIVILEGES on
// functions creates the row; from then on SVV_DEFAULT_PRIVILEGES reports whether PUBLIC kept EXECUTE.
// https://docs.aws.amazon.com/redshift/latest/dg/r_PG_DEFAULT_ACL.html
// https://docs.aws.amazon.com/redshift/latest/dg/udf-security-and-privileges.html
const (
	defaultPrivilegesImplicitPublic sqlclient.Keyword = "(SELECT privilege_type, admin_option FROM svv_default_privileges" +
		" WHERE owner_name = :owner AND (schema_name IS NULL OR schema_name = '') AND object_type = 'FUNCTION' AND grantee_type = 'public'" +
		" UNION ALL SELECT 'EXECUTE' AS privilege_type, false AS admin_option FROM pg_user u WHERE u.usename = :owner" +
		" AND NOT EXISTS (SELECT 1 FROM pg_default_acl d WHERE d.defacluser = u.usesysid AND d.defaclnamespace = 0 AND d.defaclobjtype = 'f')) AS defaults"
	defaultPrivilegesImplicitPublicCurrentUser sqlclient.Keyword = "(SELECT privilege_type, admin_option FROM svv_default_privileges" +
		" WHERE owner_name = current_user AND (schema_name IS NULL OR schema_name = '') AND object_type = 'FUNCTION' AND grantee_type = 'public'" +
		" UNION ALL SELECT 'EXECUTE' AS privilege_type, false AS admin_option FROM pg_user u WHERE u.usename = current_user" +
		" AND NOT EXISTS (SELECT 1 FROM pg_default_acl d WHERE d.defacluser = u.usesysid AND d.defaclnamespace = 0 AND d.defaclobjtype = 'f')) AS defaults"
)

// readDefaultPrivilegesQuery reads the default privileges one owner grants to one grantee for an object kind.
// An empty schema selects the database-wide defaults, which the catalog reports without a schema name, and an
// empty owner selects the connecting user's defaults.
func readDefaultPrivilegesQuery(owner, schema, catalogType, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("privilege_type", "admin_option").From("svv_default_privileges").
		WhereEither(owner != "", "owner_name = :owner", defaultPrivilegesCurrentUser, sqlclient.Bind("owner", owner)).
		WhereEither(schema != "", "schema_name = :schema", "(schema_name IS NULL OR schema_name = '')", sqlclient.Bind("schema", schema)).
		Where("object_type = :object_type", sqlclient.Bind("object_type", catalogType)).
		Where("grantee_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("grantee_type = LOWER(:kind)", sqlclient.Bind("kind", granteeType))
}

// readDefaultPrivilegesPublicQuery reads PUBLIC's database-wide function defaults including the implicit EXECUTE.
func readDefaultPrivilegesPublicQuery(owner string) sqlclient.Query {
	if owner == "" {
		return sqlclient.Select("privilege_type", "admin_option").From(defaultPrivilegesImplicitPublicCurrentUser)
	}
	return sqlclient.Select("privilege_type", "admin_option").From(defaultPrivilegesImplicitPublic, sqlclient.Bind("owner", owner))
}

// defaultPrivilegesImplicit reports the tuple Redshift grants without being asked: EXECUTE on every new function
// to PUBLIC, database-wide. Procedures run only for their owner by default, and schema defaults only add to it.
// Deleting the tuple revokes what it owns like any other and never grants the default back, so removing a lockdown
// cannot widen access.
// https://docs.aws.amazon.com/redshift/latest/dg/stored-procedure-security-and-privileges.html
func defaultPrivilegesImplicit(data types.Object) bool {
	return objectString(data, "grantee_type") == "PUBLIC" && objectString(data, "object_type") == "FUNCTIONS" && objectString(data, "schema_name") == ""
}

// defaultPrivilegesTarget validates a future-object tuple and renders its checks, catalog read, and
// ALTER DEFAULT PRIVILEGES statements.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DEFAULT_PRIVILEGES.html
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
	queries := []sqlclient.Query{localDatabaseQuery(database)}
	if owner != "" {
		queries = append(queries, privilegeUserQuery(owner))
	}
	if schemaName != "" {
		queries = append(queries, privilegeSchemaQuery(database, schemaName))
	}
	checks, err := newCatalogChecks(queries...)
	if err != nil {
		return privilegeTarget{}, err
	}
	read := readDefaultPrivilegesQuery(owner, schemaName, catalogType, objectString(data, "grantee"), objectString(data, "grantee_type"))
	if defaultPrivilegesImplicit(data) {
		read = readDefaultPrivilegesPublicQuery(owner)
	}
	query, err := newCatalogCheck(read)
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		database: database, checks: append([]catalogCheck{check}, checks...), query: query, allowed: allowed,
		grant: grantSpec{
			prefix:  sqlclient.Stmt("ALTER DEFAULT PRIVILEGES").OptIdent("FOR USER", owner).OptIdent("IN SCHEMA", schemaName),
			object:  sqlclient.Kw("ON", keyword),
			grantee: grantee,
		},
	}, nil
}
