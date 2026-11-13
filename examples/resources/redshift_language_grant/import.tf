import {
  to = redshift_language_grant.procedures
  id = jsonencode({
    database_name = "analytics"
    language_name = "PLPGSQL"
    grantee       = "developers"
    grantee_type  = "ROLE"
  })
}
