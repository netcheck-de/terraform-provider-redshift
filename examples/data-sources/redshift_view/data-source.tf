data "redshift_view" "daily_sales" {
  database = "analytics"
  schema   = "reporting"
  name     = "daily_sales"
}
