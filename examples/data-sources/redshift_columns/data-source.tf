data "redshift_columns" "daily_summary" {
  database = "analytics"
  schema   = "reporting"
  table    = "daily_summary"
}

output "required_columns" {
  value = [for column in data.redshift_columns.daily_summary.items : column.name if column.nullable == false]
}
