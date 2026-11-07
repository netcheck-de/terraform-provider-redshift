data "redshift_tables" "reporting" {
  database = "analytics"
  schema   = "reporting"
}

output "reporting_tables" {
  value = { for table in data.redshift_tables.reporting.tables : "${table.schema}.${table.name}" => table.owner }
}
