data "redshift_materialized_view" "revenue_by_region" {
  database = "analytics"
  schema   = "reporting"
  name     = "revenue_by_region"
}
