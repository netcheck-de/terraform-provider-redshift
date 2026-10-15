output "orders_table" {
  description = "Catalog definition of the example orders table, as the read-only lookup reports it."
  value = {
    id                     = data.redshift_table.orders.id
    owner                  = data.redshift_table.orders.owner
    columns                = [for column in data.redshift_table.orders.column : "${column.name} ${column.type}"]
    primary_key            = data.redshift_table.orders.primary_key.columns
    distribution           = data.redshift_table.orders.distribution
    sort_key               = data.redshift_table.orders.sort_key
    effective_distribution = data.redshift_table.orders.effective_distribution
  }
}

output "events_table_layout" {
  description = "Distribution and sort key Redshift chose for the example events table, which leaves both to AUTO."
  value = {
    distribution = redshift_table.events.effective_distribution
    sort_key     = redshift_table.events.effective_sort_key
  }
}
