data "redshift_datashare_memberships" "reporting" {
  database  = "analytics"
  datashare = "reporting"
}

output "incomplete_memberships" {
  value = [for item in data.redshift_datashare_memberships.reporting.datashare_memberships : "${item.schema}/${item.kind}" if !item.all]
}
