resource "redshift_schema" "serving" {
  database = redshift_database.warehouse.name
  name     = "serving"
  owner    = "etl"
  # Megabytes; -1 means UNLIMITED.
  quota = 51200
}
