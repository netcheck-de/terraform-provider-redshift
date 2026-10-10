data "redshift_table" "events" {
  database = "warehouse"
  schema   = "serving"
  name     = "events"
}

output "events_layout" {
  value = {
    columns      = [for column in data.redshift_table.events.column : column.name]
    distribution = data.redshift_table.events.distribution.style
    applied      = data.redshift_table.events.effective_distribution
  }
}
