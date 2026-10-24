import {
  to = redshift_role.readers
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    name           = "ncidc:analytics-readers"
  })
}
