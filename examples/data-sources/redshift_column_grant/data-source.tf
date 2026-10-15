data "redshift_column_grant" "events" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "events"
  grantee       = "analysts"
  grantee_type  = "ROLE"
}
