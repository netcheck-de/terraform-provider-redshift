data "redshift_comments" "serving_columns" {
  database_name = "analytics"
  schema_name   = "serving"
  object_type   = "COLUMN"
}

output "column_comments" {
  value = { for comment in data.redshift_comments.serving_columns.comments : "${comment.object_name}.${comment.column_name}" => comment.text }
}
