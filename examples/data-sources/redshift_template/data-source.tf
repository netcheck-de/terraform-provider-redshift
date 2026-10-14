data "redshift_template" "csv_events" {
  database = "analytics"
  schema   = "staging"
  name     = "csv_events"
}

output "csv_events_parameters" {
  value = data.redshift_template.csv_events.parameters
}
