# Database-wide and scoped grants of one database.
data "redshift_grants" "analytics" {
  database_name = "analytics"
}

# Grants on one schema and the scoped grants limited to it, for one role.
data "redshift_grants" "reporting" {
  database_name = "analytics"
  schema_name   = "reporting"
  role          = "analysts"
}

output "analytics_grants" {
  value = [for grant in data.redshift_grants.analytics.grants : "${coalesce(grant.role, grant.user, grant.datashare)} ${grant.scope}: ${join(", ", grant.privileges)}"]
}
