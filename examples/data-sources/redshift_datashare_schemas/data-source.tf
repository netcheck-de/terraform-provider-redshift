data "redshift_datashare_schemas" "reporting" {
  database  = "analytics"
  datashare = "reporting"
}

output "shared_schemas" {
  value = { for item in data.redshift_datashare_schemas.reporting.datashare_schemas : item.schema => item.include_new }
}
