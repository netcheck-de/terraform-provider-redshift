import {
  to = redshift_datashare_membership.functions
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    datashare      = "reporting"
    schema         = "reporting"
    kind           = "FUNCTIONS"
  })
}
