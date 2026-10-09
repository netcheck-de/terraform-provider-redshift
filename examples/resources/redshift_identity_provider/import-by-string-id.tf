import {
  to = redshift_identity_provider.this
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    name           = "analytics-redshift-idc"
  })
}
