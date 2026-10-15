import {
  to = redshift_rls_policy.own_region
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    name           = "own_region"
  })
}
