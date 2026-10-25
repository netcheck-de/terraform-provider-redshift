import {
  to = redshift_rls_policy.own_region
  id = jsonencode({
    database = "analytics"
    name     = "own_region"
  })
}
