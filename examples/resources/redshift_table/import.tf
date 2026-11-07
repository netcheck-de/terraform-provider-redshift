import {
  to = redshift_table.events
  id = jsonencode({
    database = "warehouse"
    schema   = "serving"
    name     = "events"
  })
}
