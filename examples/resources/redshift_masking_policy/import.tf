import {
  to = redshift_masking_policy.email
  id = jsonencode({
    database = "warehouse"
    name     = "mask_email"
  })
}
