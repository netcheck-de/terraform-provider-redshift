import {
  to = redshift_grant.readers["TABLES"]
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    database_name  = "analytics"
    role           = "ncidc:analytics-readers"
    scope          = "TABLES"
  })
}
