data "redshift_column_grants" "events" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "events"
}
