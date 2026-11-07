output "permission_checks" {
  description = "Read-only observations of column and language grants and of the object, column, and language grant listings."
  value = {
    column_privileges      = data.redshift_column_grant.reader_events.privileges
    language_privileges    = data.redshift_language_grant.loader_sql.privileges
    language_grant_options = data.redshift_language_grant.loader_sql.grant_option_privileges
    table_grants           = { for grant in data.redshift_object_grants.local_events.object_grants : "${grant.grantee_type}:${grant.grantee}" => grant.privileges }
    loader_grants          = { for grant in data.redshift_object_grants.loader.object_grants : "${grant.object_type}:${coalesce(grant.object_name, grant.schema_name, grant.database_name)}" => grant.privileges }
    column_grants          = { for grant in data.redshift_column_grants.local.column_grants : "${grant.object_name}:${grant.grantee_type}:${grant.grantee}" => grant.privileges }
    language_grants        = { for grant in data.redshift_language_grants.local.language_grants : "${grant.language_name}:${grant.grantee_type}:${grant.grantee}" => grant.grant_option_privileges }
  }
}
