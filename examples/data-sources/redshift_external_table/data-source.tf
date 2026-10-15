data "redshift_external_table" "sales" {
  database = "warehouse"
  schema   = "raw"
  name     = "sales"
}
