import {
  to = redshift_table.events
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    schema         = "serving"
    name           = "events"
  })
}
