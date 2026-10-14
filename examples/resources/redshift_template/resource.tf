resource "redshift_template" "csv_events" {
  database = redshift_schema.staging.database
  schema   = redshift_schema.staging.name
  name     = "csv_events"
  format   = "CSV"

  parameters = {
    IGNOREHEADER = "1"
    DATEFORMAT   = "auto"
    GZIP         = ""
    ENCODING     = "UTF8"
  }
}

# JSON and AVRO take a format argument; MAXERROR is allowed in templates but not in copy jobs.
resource "redshift_template" "json_events" {
  database        = redshift_schema.staging.database
  schema          = redshift_schema.staging.name
  name            = "json_events"
  owner           = "loader"
  format          = "JSON"
  format_argument = "auto ignorecase"

  parameters = {
    MAXERROR        = "100"
    TRUNCATECOLUMNS = ""
  }
}
