data "redshift_datashare_schema" "reporting" {
  database  = "analytics"
  datashare = "reports"
  schema    = "reporting"
}
