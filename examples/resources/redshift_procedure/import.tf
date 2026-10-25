import {
  to = redshift_procedure.purge
  id = jsonencode({
    database  = "analytics"
    schema    = "reporting"
    name      = "sp_purge_events"
    arguments = "INTEGER"
  })
}
