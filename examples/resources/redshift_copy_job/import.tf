import {
  to = redshift_copy_job.events
  id = jsonencode({
    database = "analytics"
    name     = "events_auto_load"
  })
}
