output "grant_checks" {
  description = "Observed user grant options, schema snapshot privileges, the implicit PUBLIC EXECUTE default on new functions, and the scoped, system, ASSUMEROLE, and default privilege listings."
  value = {
    loader_tables             = data.redshift_grant.loader_schema_tables.privileges
    loader_table_options      = data.redshift_grant.loader_schema_tables.grant_option_privileges
    operators_snapshot        = data.redshift_object_grant.operators_public_tables.privileges
    public_function_execs     = data.redshift_default_privilege.loader_functions_public.privileges
    public_language_usage     = data.redshift_language_grant.public_sql.privileges
    shared_reader             = { for kind, grant in data.redshift_object_grant.shared_reader : kind => grant.privileges }
    offload_commands          = var.enable_assumerole_grant ? data.redshift_assumerole_grant.operators_any_role[0].privileges : toset([])
    shared_function           = data.redshift_object_grant.shared_function.privileges
    shared_function_options   = data.redshift_object_grant.shared_function.grant_option_privileges
    shared_snapshots          = { for kind, grant in data.redshift_object_grant.shared_snapshots : kind => { privileges = grant.privileges, options = grant.grant_option_privileges } }
    shared_procedure          = data.redshift_object_grant.shared_procedure.privileges
    shared_procedure_snapshot = data.redshift_object_grant.shared_procedure_snapshot.privileges
    local_schema_grants       = { for grant in data.redshift_grants.local_schema.grants : "${grant.scope}:${coalesce(grant.role, grant.user, grant.datashare)}" => grant.privileges }
    operator_system_grants    = { for grant in data.redshift_system_grants.operators.system_grants : grant.role => grant.privileges }
    operator_role_commands    = { for grant in data.redshift_assumerole_grants.operators.assumerole_grants : grant.iam_role_arn => grant.privileges }
    loader_default_privileges = [for rule in data.redshift_default_privileges.loader.default_privileges : "${coalesce(rule.schema_name, "*")}:${rule.object_type}:${rule.grantee_type}:${rule.grantee}"]
  }
}
