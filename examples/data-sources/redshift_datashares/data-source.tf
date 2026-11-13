data "redshift_datashares" "all" {}

data "redshift_datashares" "reporting" {
  name = "reporting"
}

output "outbound_datashare_databases" {
  value = { for share in data.redshift_datashares.all.datashares : share.name => share.database }
}
