data "redshift_grant" "readers" {
  database_name = "analytics"
  role          = "report_readers"
  scope         = "TABLES"
  schema_name   = "reporting"
}
