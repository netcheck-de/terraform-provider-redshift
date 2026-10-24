import {
  to = redshift_policy_grant.exempt_lookup
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    database_name  = "warehouse"
    schema_name    = "public"
    object_name    = "masking_exempt_emails"
    policy_type    = "MASKING"
    policy_name    = "mask_email"
  })
}
