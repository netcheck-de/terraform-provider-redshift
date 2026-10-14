import {
  to = redshift_template.csv_events
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "staging"
    name           = "csv_events"
  })
}
