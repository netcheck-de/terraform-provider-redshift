data "redshift_external_tables" "raw" {
  database = "warehouse"
  schema   = "raw"
}

output "external_table_locations" {
  value = { for table in data.redshift_external_tables.raw.external_tables : table.name => table.location }
}
