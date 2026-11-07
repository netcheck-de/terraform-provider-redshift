data "redshift_external_partitions" "sales" {
  database = "warehouse"
  schema   = "raw"
  table    = "sales"
}

output "sales_partitions" {
  value = { for partition in data.redshift_external_partitions.sales.external_partitions : partition.values.sale_date => partition.location }
}
