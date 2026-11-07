data "redshift_datashare_tables" "reporting" {
  database  = "analytics"
  datashare = "reporting"
  schema    = "public"
}

output "shared_tables" {
  value = data.redshift_datashare_tables.reporting.datashare_tables[*].table
}
