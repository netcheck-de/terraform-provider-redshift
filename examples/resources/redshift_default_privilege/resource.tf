resource "redshift_default_privilege" "reports" {
  database_name = "analytics"
  owner         = redshift_user.loader.name
  schema_name   = "reporting"
  object_type   = "TABLES"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]
}
