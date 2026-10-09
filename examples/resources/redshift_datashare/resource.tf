resource "redshift_datashare" "analytics" {
  database = redshift_database.warehouse.name
  name     = "analytics"
}
