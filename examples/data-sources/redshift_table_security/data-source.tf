data "redshift_table_security" "orders" {
  database = "analytics"
  schema   = "sales"
  relation = "orders"
}
