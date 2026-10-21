data "redshift_datashare_membership" "functions" {
  database  = "analytics"
  datashare = "reporting"
  schema    = "reporting"
  kind      = "FUNCTIONS"
}
