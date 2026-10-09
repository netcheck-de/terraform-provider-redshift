data "redshift_datashare_table" "report" {
  database  = "analytics"
  datashare = "reports"
  schema    = "reporting"
  table     = "daily_summary"
}
