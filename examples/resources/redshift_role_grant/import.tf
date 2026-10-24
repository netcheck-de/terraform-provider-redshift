import {
  to = redshift_role_grant.operators
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    role           = "sys:dba"
    to_role        = "ncidc:analytics-operators"
  })
}
