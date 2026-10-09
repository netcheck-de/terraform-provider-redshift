data "redshift_comment" "column" {
  database_name = "analytics"
  object_type   = "COLUMN"
  object_name   = "daily_summary"
  schema_name   = "reporting"
  column_name   = "day"
}
