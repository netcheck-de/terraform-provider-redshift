import {
  to = redshift_materialized_view.revenue_by_region
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "reporting"
    name           = "revenue_by_region"
  })
}
