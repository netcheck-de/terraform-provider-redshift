import {
  to = redshift_materialized_view.revenue_by_region
  id = jsonencode({
    database = "analytics"
    schema   = "reporting"
    name     = "revenue_by_region"
  })
}
