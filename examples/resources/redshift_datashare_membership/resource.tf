resource "redshift_datashare_membership" "functions" {
  database  = redshift_datashare_schema.reporting.database
  datashare = redshift_datashare_schema.reporting.datashare
  schema    = redshift_datashare_schema.reporting.schema
  kind      = "FUNCTIONS"
  all       = true

  # The shared schema must have include_new = false. Do not manage the same kind with per-object members or grants.
  depends_on = [redshift_function.greater]
}
