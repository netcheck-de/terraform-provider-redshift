import {
  to = redshift_masking_policy.email
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    name           = "mask_email"
  })
}
