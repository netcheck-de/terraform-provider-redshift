data "redshift_object_grant" "report" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  object_type   = "TABLE"
  grantee       = "report_readers"
  grantee_type  = "GROUP"
}
