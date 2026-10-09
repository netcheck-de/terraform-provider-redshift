resource "redshift_datashare_table" "view" {
  database  = redshift_datashare_schema.serving.database
  datashare = redshift_datashare_schema.serving.datashare
  schema    = redshift_datashare_schema.serving.schema
  table     = "example_view"
}
