package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// databasesSource resolves owner names in the same statement. SVV_REDSHIFT_DATABASES reports the owner as a user
// ID, and users are warehouse-wide, so pg_user in the administration database knows every local owner.
const databasesSource sqlclient.Keyword = "svv_redshift_databases d LEFT JOIN pg_user u ON u.usesysid = d.database_owner"

// readDatabasesQuery lists local and datashare databases. The type is compared in lower case because the view
// documents lower-case values while SHOW output elsewhere is upper case; nameLike is a LIKE pattern bound as a
// parameter, so wildcards apply but quoting cannot be broken.
func readDatabasesQuery(databaseType, nameLike string) sqlclient.Query {
	return sqlclient.Select("d.database_name", "u.usename AS owner", "LOWER(d.database_type) AS database_type", "d.database_isolation_level AS isolation_level").
		From(databasesSource).
		OptEq("LOWER(d.database_type)", "database_type", databaseType).
		WhereIf(nameLike != "", "d.database_name LIKE :name_like", sqlclient.Bind("name_like", nameLike)).
		OrderBy("d.database_name")
}
