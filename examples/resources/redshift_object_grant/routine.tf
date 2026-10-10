resource "redshift_object_grant" "score" {
  database_name           = "analytics"
  schema_name             = "reporting"
  object_name             = "f_score"
  object_type             = "FUNCTION"
  arguments               = "integer, varchar"
  grantee                 = redshift_user.analyst.name
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}

resource "redshift_object_grant" "reporting_tables" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_type   = "ALL TABLES"
  grantee       = redshift_role.readers.name
  grantee_type  = "ROLE"
  privileges    = ["SELECT"]
}
