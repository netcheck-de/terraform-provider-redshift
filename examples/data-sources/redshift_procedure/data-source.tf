data "redshift_procedure" "purge" {
  database  = "analytics"
  schema    = "reporting"
  name      = "sp_purge_events"
  arguments = [{ type = "integer" }]
}
