package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// tablesSource lists local and datashare tables with their owners across databases; external tables are not in it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_REDSHIFT_TABLES.html
const tablesSource sqlclient.Keyword = "svv_redshift_tables"

// listTablesQuery lists the tables of one database, optionally in one schema. table_type = 'TABLE' leaves out views
// and materialized views; schemas starting with pg_ and information_schema hold only system tables.
func listTablesQuery(database, schemaName string) sqlclient.Query {
	return sqlclient.Select("schema_name", "table_name", "table_owner").
		From(tablesSource).
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("table_type = 'TABLE'").
		Where("schema_name !~ '^pg_'").
		Where("schema_name <> 'information_schema'").
		OptEq("schema_name", "schema", schemaName).
		OrderBy("schema_name", "table_name")
}
