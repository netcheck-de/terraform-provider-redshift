data "redshift_datashares" "all" {}

data "redshift_datashares" "outbound" {
  share_type = "OUTBOUND"
}

output "outbound_datashare_names" {
  value = data.redshift_datashares.outbound.datashares[*].name
}
