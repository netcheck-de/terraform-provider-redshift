data "redshift_external_partition" "sales_2008_01" {
  database = "warehouse"
  schema   = "raw"
  table    = "sales"
  values   = { sale_date = "2008-01-01" }
}
