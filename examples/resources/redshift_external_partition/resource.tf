resource "redshift_external_partition" "sales_2008_01" {
  database = redshift_external_table.sales.database
  schema   = redshift_external_table.sales.schema
  table    = redshift_external_table.sales.name
  values   = { sale_date = "2008-01-01" }
  location = "s3://example-bucket/tickit/sales/sale_date=2008-01-01/"
}
