import {
  to = redshift_table_security.orders
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "sales"
    relation       = "orders"
  })
}
