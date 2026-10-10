import {
  to = redshift_view.daily_sales
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "reporting"
    name           = "daily_sales"
  })
}
