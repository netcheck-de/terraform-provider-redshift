data "redshift_tables" "materialized" {
  database   = "analytics"
  schema     = "reporting"
  table_type = "MATERIALIZED VIEW"
}

output "materialized_views" {
  value = [for table in data.redshift_tables.materialized.tables : "${table.schema}.${table.name}"]
}
