data "redshift_language_grant" "developers_plpgsql" {
  database_name = "analytics"
  language_name = "PLPGSQL"
  grantee       = "developers"
  grantee_type  = "ROLE"
}
