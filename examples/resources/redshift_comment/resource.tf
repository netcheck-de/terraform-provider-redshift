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

resource "redshift_comment" "primary_key" {
  database_name   = "analytics"
  object_type     = "CONSTRAINT"
  schema_name     = "reporting"
  object_name     = "daily_summary"
  constraint_name = "daily_summary_pkey"
  text            = "One row per reporting day."
}
