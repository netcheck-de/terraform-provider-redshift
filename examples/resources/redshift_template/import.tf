import {
  to = redshift_template.csv_events
  id = jsonencode({
    database = "analytics"
    schema   = "staging"
    name     = "csv_events"
  })
}
