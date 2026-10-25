import {
  to = redshift_table_security.orders
  id = jsonencode({
    database = "analytics"
    schema   = "sales"
    relation = "orders"
  })
}
