output "grant_checks" {
  description = "Observed user grant options, schema snapshot privileges, and the implicit PUBLIC EXECUTE default on new functions."
  value = {
    loader_tables         = data.redshift_grant.loader_schema_tables.privileges
    loader_table_options  = data.redshift_grant.loader_schema_tables.grant_option_privileges
    operators_snapshot    = data.redshift_object_grant.operators_public_tables.privileges
    public_function_execs = data.redshift_default_privileges.loader_functions_public.privileges
  }
}
