data "redshift_table" "events" {
  database = "warehouse"
  schema   = "serving"
  name     = "events"
}
