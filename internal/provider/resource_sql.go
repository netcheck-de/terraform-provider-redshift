package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// localDatabaseQuery confirms that a database exists and is local, since shared databases reject DDL and grants.
func localDatabaseQuery(database string) sqlclient.Query {
	return sqlclient.Select("database_name").From("svv_redshift_databases").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("database_type = 'local'")
}
