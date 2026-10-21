output "grant_checks" {
  description = "Observed user grant options, schema snapshot privileges, and the implicit PUBLIC EXECUTE default on new functions."
  value = {
    loader_tables             = data.redshift_grant.loader_schema_tables.privileges
    loader_table_options      = data.redshift_grant.loader_schema_tables.grant_option_privileges
    operators_snapshot        = data.redshift_object_grant.operators_public_tables.privileges
    public_function_execs     = data.redshift_default_privileges.loader_functions_public.privileges
    public_language_usage     = data.redshift_language_grant.public_sql.privileges
    shared_reader             = { for kind, grant in data.redshift_object_grant.shared_reader : kind => grant.privileges }
    offload_commands          = var.enable_assumerole_grant ? data.redshift_assumerole_grant.operators_any_role[0].privileges : toset([])
    shared_function           = data.redshift_object_grant.shared_function.privileges
    shared_function_options   = data.redshift_object_grant.shared_function.grant_option_privileges
    shared_snapshots          = { for kind, grant in data.redshift_object_grant.shared_snapshots : kind => { privileges = grant.privileges, options = grant.grant_option_privileges } }
    shared_procedure          = data.redshift_object_grant.shared_procedure.privileges
    shared_procedure_snapshot = data.redshift_object_grant.shared_procedure_snapshot.privileges
  }
}
