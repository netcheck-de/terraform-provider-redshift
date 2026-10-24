import {
  to = redshift_masking_policy_attachment.email_analysts
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    policy         = "mask_email"
    schema         = "public"
    relation       = "customers"
    columns        = jsonencode(["email"])
    grantee        = "analysts"
    grantee_type   = "ROLE"
  })
}
