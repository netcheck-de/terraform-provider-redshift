data "redshift_language_grants" "analytics" {
  database_name = "analytics"
}

output "language_grants" {
  value = [for grant in data.redshift_language_grants.analytics.language_grants : "${grant.grantee_type} ${grant.grantee}: ${grant.language_name}"]
}
