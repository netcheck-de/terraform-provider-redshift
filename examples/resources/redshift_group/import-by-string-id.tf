import {
  to = redshift_group.readers
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    name           = "report_readers"
  })
}
