resource "redshift_comment" "schema" {
  database_name = redshift_schema.reporting.database
  object_type   = "SCHEMA"
  object_name   = redshift_schema.reporting.name
  text          = "Curated reporting objects."
}

resource "redshift_comment" "column" {
  database_name = "analytics"
  object_type   = "COLUMN"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  column_name   = "day"
  text          = "UTC reporting date."
}
