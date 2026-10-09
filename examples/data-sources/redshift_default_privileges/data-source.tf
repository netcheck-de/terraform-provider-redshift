data "redshift_default_privileges" "reports" {
  database_name = "analytics"
  owner         = "report_loader"
  schema_name   = "reporting"
  object_type   = "TABLES"
  grantee       = "report_readers"
  grantee_type  = "GROUP"
}
