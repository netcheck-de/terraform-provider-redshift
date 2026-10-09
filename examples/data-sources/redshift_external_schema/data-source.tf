data "redshift_external_schema" "raw" {
  database = "warehouse"
  name     = "raw"
}
