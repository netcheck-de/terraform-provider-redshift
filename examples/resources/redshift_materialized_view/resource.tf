resource "redshift_materialized_view" "revenue_by_region" {
  database     = redshift_schema.reporting.database
  schema       = redshift_schema.reporting.name
  name         = "revenue_by_region"
  backup       = false
  auto_refresh = true
  query        = <<-SQL
    SELECT region, SUM(amount) AS revenue
    FROM reporting.sales
    GROUP BY region
  SQL

  distribution {
    style = "KEY"
    key   = "region"
  }

  sort_key {
    columns = ["region"]
  }
}
