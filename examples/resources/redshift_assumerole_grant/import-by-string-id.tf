import {
  to = redshift_assumerole_grant.loader
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    iam_role_arn   = "DEFAULT"
    grantee        = "loader"
    grantee_type   = "ROLE"
  })
}
