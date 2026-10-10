package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// Table types reported by redshift_tables. SVV_ALL_TABLES documents views, base tables, external tables and shared
// tables; materialized views appear there as views and are told apart through SVV_MV_INFO.
const (
	tablesTypeTable        = "TABLE"
	tablesTypeView         = "VIEW"
	tablesTypeMaterialized = "MATERIALIZED VIEW"
	tablesTypeExternal     = "EXTERNAL TABLE"
	tablesTypeShared       = "SHARED TABLE"
)

// tablesTypes lists the accepted table_type filter values.
var tablesTypes = []string{tablesTypeTable, tablesTypeView, tablesTypeMaterialized, tablesTypeExternal, tablesTypeShared}

// tablesSource adds the owner, which only SVV_REDSHIFT_TABLES reports; external tables keep a null owner.
const tablesSource sqlclient.Keyword = "svv_all_tables t LEFT JOIN svv_redshift_tables r ON r.database_name = t.database_name AND r.schema_name = t.schema_name AND r.table_name = t.table_name"

// tablesCatalogType maps a table_type filter to the type SVV_ALL_TABLES stores, where materialized views are views.
func tablesCatalogType(tableType string) string {
	if tableType == tablesTypeMaterialized {
		return tablesTypeView
	}
	return tableType
}

// tablesNeedMaterialized reports whether the listing can contain views, the only rows SVV_MV_INFO reclassifies.
func tablesNeedMaterialized(tableType string) bool {
	return tableType == "" || tablesCatalogType(tableType) == tablesTypeView
}

// readTablesQuery lists the relations of one database, optionally in one schema and of one catalog type. The
// type is upper-cased because the view does not document its letter case.
func readTablesQuery(database, schemaName, tableType string) sqlclient.Query {
	return sqlclient.Select("t.schema_name", "t.table_name", "UPPER(t.table_type) AS table_type", "r.table_owner AS owner", "t.remarks").
		From(tablesSource).
		Where("t.database_name = :database", sqlclient.Bind("database", database)).
		OptEq("t.schema_name", "schema", schemaName).
		OptEq("UPPER(t.table_type)", "table_type", tablesCatalogType(tableType)).
		OrderBy("t.schema_name", "t.table_name")
}

// readTablesMaterializedQuery lists the materialized views of one database. SVV_MV_INFO stores names as CHAR(128),
// so they are trimmed before comparison. It stays a separate query because SVV_MV_INFO is built on STV state,
// and Redshift rejects some joins between such views and the leader-node catalog views of the listing.
func readTablesMaterializedQuery(database, schemaName string) sqlclient.Query {
	return sqlclient.Select("TRIM(schema_name) AS schema_name", "TRIM(name) AS name").
		From("svv_mv_info").
		Where("TRIM(database_name) = :database", sqlclient.Bind("database", database)).
		OptEq("TRIM(schema_name)", "schema", schemaName)
}
