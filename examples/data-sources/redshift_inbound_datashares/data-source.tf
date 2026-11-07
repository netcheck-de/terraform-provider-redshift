data "redshift_inbound_datashares" "all" {}

output "unconsumed_datashares" {
  value = [for share in data.redshift_inbound_datashares.all.inbound_datashares : share.name if share.consumer_database == null]
}
