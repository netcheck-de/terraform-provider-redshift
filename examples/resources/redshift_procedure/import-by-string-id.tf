import {
  to = redshift_procedure.purge
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "reporting"
    name           = "sp_purge_events"
    arguments      = "integer"
  })
}
