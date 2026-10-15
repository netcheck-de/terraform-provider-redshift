output "permission_checks" {
  description = "Read-only observations of column and language grants and of the grant listings."
  value = {
    column_privileges      = data.redshift_column_grant.reader_events.privileges
    language_privileges    = data.redshift_language_grant.loader_sql.privileges
    language_grant_options = data.redshift_language_grant.loader_sql.grant_option_privileges
    table_grants           = [for grant in data.redshift_grants.local_events.grants : "${grant.grantee_type}:${grant.grantee}:${grant.privilege}"]
    loader_grants          = { for grant in data.redshift_grants.loader.grants : grant.privilege => grant.object_name... }
    column_grants          = [for grant in data.redshift_column_grants.local.column_grants : "${grant.object_name}.${grant.column_name}:${grant.privilege}:${grant.grantee}"]
  }
}
