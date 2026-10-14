resource "redshift_copy_job" "events" {
  database = redshift_table.events.database
  name     = "events_auto_load"
  source   = "s3://example-bucket/events/"
  iam_role = "arn:aws:iam::123456789012:role/redshift-loader"
  format   = "CSV"

  parameters = {
    IGNOREHEADER = "1"
    TIMEFORMAT   = "auto"
  }

  table {
    schema  = redshift_table.events.schema
    name    = redshift_table.events.name
    columns = ["id", "label", "created_at"]
  }
}

# AUTO OFF keeps the job defined without loading files automatically.
resource "redshift_copy_job" "archive" {
  database        = redshift_table.events.database
  name            = "events_archive_load"
  source          = "s3://example-bucket/archive/"
  iam_role        = "arn:aws:iam::123456789012:role/redshift-loader"
  format          = "JSON"
  format_argument = "auto"
  auto            = false

  table {
    schema = redshift_table.events.schema
    name   = redshift_table.events.name
  }
}
