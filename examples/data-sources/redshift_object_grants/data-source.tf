# Every grant on one table, including group and PUBLIC grantees.
data "redshift_object_grants" "events" {
  database_name = "analytics"
  object_type   = "TABLE"
  schema_name   = "reporting"
  object_name   = "events"
}

# Every object grant one role holds in one database, narrowed to one schema.
data "redshift_object_grants" "analysts" {
  database_name = "analytics"
  grantee       = "analysts"
  grantee_type  = "ROLE"
  schema_name   = "reporting"
}

output "events_grants" {
  value = { for grant in data.redshift_object_grants.events.object_grants : "${grant.grantee_type} ${grant.grantee}" => grant.privileges }
}
