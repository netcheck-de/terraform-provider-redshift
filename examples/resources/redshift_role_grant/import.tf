import {
  to = redshift_role_grant.operators
  id = jsonencode({
    role    = "sys:dba"
    to_role = "ncidc:analytics-operators"
  })
}
