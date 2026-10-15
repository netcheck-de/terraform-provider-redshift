data "redshift_constraints" "orders_keys" {
  database        = "analytics"
  schema          = "reporting"
  table           = "orders"
  constraint_type = "PRIMARY KEY"
}

resource "redshift_comment" "orders_key" {
  for_each = { for key in data.redshift_constraints.orders_keys.constraints : key.name => key }

  database_name   = each.value.database
  object_type     = "CONSTRAINT"
  schema_name     = each.value.schema
  object_name     = each.value.table
  constraint_name = each.value.name
  text            = "Business key: ${join(", ", each.value.columns)}."
}
