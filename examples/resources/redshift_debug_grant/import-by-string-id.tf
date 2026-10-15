import {
  to = redshift_debug_grant.auditor
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "sales_db@finance-catalog"
    grantee        = "IAM:sales_auditor"
    grantee_type   = "USER"
  })
}
