import {
  to = redshift_assumerole_grant.loader
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    iam_role_arn   = "default"
    grantee        = "loader"
    grantee_type   = "ROLE"
  })
}
