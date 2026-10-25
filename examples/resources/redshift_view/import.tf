import {
  to = redshift_view.daily_sales
  id = jsonencode({
    database = "analytics"
    schema   = "reporting"
    name     = "daily_sales"
  })
}
