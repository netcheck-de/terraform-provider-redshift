resource "redshift_schema" "serving" {
  database = redshift_database.warehouse.name
  name     = "serving"
}
