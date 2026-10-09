import {
  to = redshift_datashare.analytics
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    name           = "analytics"
  })
}
