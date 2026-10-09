data "redshift_schema" "serving" {
  database = "warehouse"
  name     = "serving"
}
