resource "redshift_view" "daily_sales" {
  database = redshift_schema.reporting.database
  schema   = redshift_schema.reporting.name
  name     = "daily_sales"
  owner    = "reporting_owner"
  query    = <<-SQL
    SELECT sale_date, SUM(amount) AS revenue
    FROM reporting.sales
    GROUP BY sale_date
  SQL
}

# A late-binding view may reference tables that do not exist yet, such as Redshift Spectrum tables.
resource "redshift_view" "all_sales" {
  database     = redshift_schema.reporting.database
  schema       = redshift_schema.reporting.name
  name         = "all_sales"
  late_binding = true
  query        = "SELECT * FROM reporting.sales UNION ALL SELECT * FROM spectrum.sales_archive"
}
