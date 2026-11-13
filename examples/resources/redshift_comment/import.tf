import {
  to = redshift_comment.column
  id = jsonencode({
    database_name = "analytics"
    object_type   = "COLUMN"
    schema_name   = "reporting"
    object_name   = "daily_summary"
    column_name   = "day"
  })
}
