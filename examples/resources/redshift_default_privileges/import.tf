import {
  to = redshift_default_privileges.reports
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    database_name  = "analytics"
    owner          = "loader"
    schema_name    = "reporting"
    object_type    = "TABLES"
    grantee        = "report_readers"
    grantee_type   = "GROUP"
  })
}
