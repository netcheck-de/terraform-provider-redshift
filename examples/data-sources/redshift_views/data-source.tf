data "redshift_views" "reporting" {
  database = "analytics"
  schema   = "reporting"
}

output "late_binding_views" {
  value = [for view in data.redshift_views.reporting.views : "${view.schema}.${view.name}" if view.late_binding]
}
