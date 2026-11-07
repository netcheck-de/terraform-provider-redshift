import {
  to = redshift_column_grant.events
  id = jsonencode({
    database_name = "analytics"
    schema_name   = "reporting"
    object_name   = "events"
    grantee       = "analysts"
    grantee_type  = "ROLE"
  })
}
