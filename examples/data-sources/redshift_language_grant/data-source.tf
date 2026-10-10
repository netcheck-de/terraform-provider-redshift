data "redshift_language_grant" "developers_plpgsql" {
  database_name = "analytics"
  language_name = "plpgsql"
  grantee       = "developers"
  grantee_type  = "ROLE"
}
