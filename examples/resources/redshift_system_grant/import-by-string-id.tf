import {
  to = redshift_system_grant.operators
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    role           = "operators"
  })
}
