data "redshift_datashare_grants" "reporting" {
  datashare = "reporting"
}

output "consumer_namespaces" {
  value = compact(data.redshift_datashare_grants.reporting.datashare_grants[*].namespace_id)
}
