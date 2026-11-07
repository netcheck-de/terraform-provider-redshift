data "redshift_materialized_views" "reporting" {
  database = "analytics"
  schema   = "reporting"
}

output "manually_refreshed_views" {
  value = [for view in data.redshift_materialized_views.reporting.materialized_views : "${view.schema}.${view.name}" if view.auto_refresh == false]
}
