# Every grant on one table, including group and PUBLIC grantees.
data "redshift_grants" "events" {
  database_name = "analytics"
  object_type   = "TABLE"
  schema_name   = "reporting"
  object_name   = "events"
}

# Every grant one role holds in one database, narrowed to one schema.
data "redshift_grants" "analysts" {
  database_name = "analytics"
  grantee       = "analysts"
  grantee_type  = "ROLE"
  schema_name   = "reporting"
}
