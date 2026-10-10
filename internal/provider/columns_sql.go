package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// readColumnsQuery lists the columns of one database's relations in table order. SVV_ALL_COLUMNS documents
// is_nullable as yes or no while samples print YES and NO, so it is upper-cased.
func readColumnsQuery(database, schemaName, table string) sqlclient.Query {
	return sqlclient.Select("schema_name", "table_name", "column_name", "ordinal_position", "data_type", "character_maximum_length", "numeric_precision", "numeric_scale", "UPPER(is_nullable) AS is_nullable", "column_default", "remarks").
		From("svv_all_columns").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		OptEq("schema_name", "schema", schemaName).
		OptEq("table_name", "table", table).
		OrderBy("schema_name", "table_name", "ordinal_position")
}
