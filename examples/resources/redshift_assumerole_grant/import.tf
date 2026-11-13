import {
  to = redshift_assumerole_grant.loader
  id = jsonencode({
    iam_role_arn = "DEFAULT"
    grantee      = "loader"
    grantee_type = "ROLE"
  })
}
