package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// schemasSource resolves owner names for local and external schemas. A shared schema's owner ID belongs to the
// producer warehouse, where it may name a different local user, so it is never resolved.
const schemasSource sqlclient.Keyword = "svv_all_schemas s LEFT JOIN pg_user u ON u.usesysid = s.schema_owner AND LOWER(s.schema_type) <> 'shared'"

// readSchemasQuery lists the schemas of one database. SVV_ALL_SCHEMAS documents lower-case types while SHOW
// SCHEMAS prints EXTERNAL in upper case, so the type is compared in lower case; callers report it in upper case.
func readSchemasQuery(database, schemaType string) sqlclient.Query {
	return sqlclient.Select("s.schema_name", "u.usename AS owner", "LOWER(s.schema_type) AS schema_type", "s.source_database").
		From(schemasSource).
		Where("s.database_name = :database", sqlclient.Bind("database", database)).
		OptEq("LOWER(s.schema_type)", "schema_type", schemaType).
		OrderBy("s.schema_name")
}
