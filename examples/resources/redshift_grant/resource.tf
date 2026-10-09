resource "redshift_grant" "readers" {
  for_each = {
    DATABASE = ["USAGE"]
    SCHEMAS  = ["USAGE"]
    TABLES   = ["SELECT"]
  }

  database_name = redshift_database.analytics.name
  role          = redshift_role.readers.name
  scope         = each.key
  privileges    = each.value
}
