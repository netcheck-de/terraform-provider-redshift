data "redshift_datashare" "analytics" {
  database = "warehouse"
  name     = "analytics"
}
