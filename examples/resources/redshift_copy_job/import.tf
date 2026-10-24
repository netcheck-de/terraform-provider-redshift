import {
  to = redshift_copy_job.events
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    name           = "events_auto_load"
  })
}
