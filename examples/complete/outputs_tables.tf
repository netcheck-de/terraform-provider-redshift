output "orders_table" {
  description = "Catalog definition of the example orders table, as the read-only lookup reports it."
  value = {
    id            = data.redshift_table.orders.id
    owner         = data.redshift_table.orders.owner
    columns       = [for column in data.redshift_table.orders.columns : "${column.name} ${column.type}"]
    primary_key   = data.redshift_table.orders.primary_key
    diststyle     = data.redshift_table.orders.diststyle
    distkey       = data.redshift_table.orders.distkey
    sortkey_style = data.redshift_table.orders.sortkey_style
    sortkey       = data.redshift_table.orders.sortkey
  }
}
