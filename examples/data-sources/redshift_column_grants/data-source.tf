data "redshift_column_grants" "events" {
  database_name = "analytics"
  schema_name   = "reporting"
  object_name   = "events"
}

output "events_column_grants" {
  value = [for grant in data.redshift_column_grants.events.column_grants : "${grant.grantee}: ${grant.privilege} (${grant.column_name})"]
}
