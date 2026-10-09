resource "redshift_object_grant" "report" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "daily_summary"
  object_type   = "TABLE"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]
}
