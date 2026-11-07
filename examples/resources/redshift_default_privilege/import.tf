import {
  to = redshift_default_privilege.reports
  id = jsonencode({
    database_name = "analytics"
    owner         = "loader"
    schema_name   = "reporting"
    object_type   = "TABLES"
    grantee       = "report_readers"
    grantee_type  = "GROUP"
  })
}
