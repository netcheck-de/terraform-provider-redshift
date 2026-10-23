# Mocked runs for scoped, object, default, system, and ASSUMEROLE grants. The mocks are shared with composition.tftest.hcl through tests/mocks, so
# this block adds runs and assertions here without editing the composition suite.
mock_provider "aws" {
  alias  = "producer"
  source = "./tests/mocks/aws_producer"
}

mock_provider "aws" {
  alias  = "consumer"
  source = "./tests/mocks/aws_consumer"
}

mock_provider "random" {
  source = "./tests/mocks/random"
}

mock_provider "redshift" { alias = "producer" }
mock_provider "redshift" {
  alias  = "consumer"
  source = "./tests/mocks/redshift_consumer"
}
mock_provider "redshift" { alias = "producer_data_api_iam" }
mock_provider "redshift" { alias = "consumer_data_api_iam" }
mock_provider "redshift" { alias = "producer_direct_iam" }
mock_provider "redshift" { alias = "consumer_direct_iam" }
mock_provider "redshift" { alias = "producer_direct_password" }
mock_provider "redshift" { alias = "consumer_direct_password" }

override_data {
  target = data.redshift_grant.loader_schema_tables
  values = { privileges = ["INSERT", "SELECT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_object_grant.operators_public_tables
  values = { privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_default_privileges.loader_functions_public
  values = { privileges = [] }
}

override_data {
  target = data.redshift_language_grant.public_sql
  values = { privileges = [] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["DATABASE"]
  values = { privileges = ["USAGE"] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["SCHEMA"]
  values = { privileges = ["USAGE"] }
}
override_data {
  target = data.redshift_object_grant.shared_reader["TABLE"]
  values = { privileges = ["SELECT"] }
}

override_data {
  target = data.redshift_assumerole_grant.operators_any_role[0]
  values = { privileges = ["UNLOAD", "RESULT_OFFLOAD"] }
}

override_data {
  target = data.redshift_object_grant.shared_function
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_object_grant.shared_snapshots["ALL TABLES"]
  values = { privileges = ["SELECT"], grant_option_privileges = ["SELECT"] }
}
override_data {
  target = data.redshift_object_grant.shared_snapshots["ALL FUNCTIONS"]
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}

override_data {
  target = data.redshift_object_grant.shared_procedure
  values = { privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_object_grant.shared_procedure_snapshot
  values = { privileges = ["EXECUTE"] }
}

run "grants_apply" {
  command = apply

  assert {
    condition = (
      redshift_default_privileges.loader_functions_public.restore_default_on_destroy &&
      contains(redshift_assumerole_grant.operators_any_role[0].privileges, "RESULT_OFFLOAD") &&
      output.grant_checks.offload_commands == toset(["UNLOAD", "RESULT_OFFLOAD"]) &&
      redshift_object_grant.shared_function.arguments == null &&
      output.grant_checks.shared_function == toset(["EXECUTE"]) &&
      output.grant_checks.shared_function_options == toset(["EXECUTE"]) &&
      output.grant_checks.shared_snapshots["ALL TABLES"].privileges == toset(["SELECT"]) &&
      output.grant_checks.shared_snapshots["ALL FUNCTIONS"].options == toset(["EXECUTE"]) &&
      redshift_object_grant.shared_procedure.object_type == "PROCEDURE" &&
      redshift_object_grant.shared_procedure_snapshot.object_type == "ALL PROCEDURES" &&
      output.grant_checks.shared_procedure == toset(["EXECUTE"]) &&
      output.grant_checks.shared_procedure_snapshot == toset(["EXECUTE"]) &&
      redshift_language_grant.public_sql.restore_default_on_destroy &&
      length(output.grant_checks.public_language_usage) == 0 &&
      alltrue([for grant in redshift_object_grant.shared_reader : grant.database_type == "shared" && grant.grantee == redshift_user.reader.name]) &&
      output.grant_checks.shared_reader["DATABASE"] == toset(["USAGE"]) &&
      output.grant_checks.shared_reader["SCHEMA"] == toset(["USAGE"]) &&
      output.grant_checks.shared_reader["TABLE"] == toset(["SELECT"])
    )
    error_message = "PUBLIC cleanup restoration and distinct shared object grants must be configured and observed."
  }

  assert {
    condition = (
      redshift_grant.loader_schema_tables.user == redshift_user.loader.name && redshift_grant.loader_schema_tables.role == null &&
      redshift_grant.loader_schema_tables.grant_option_privileges == toset(["SELECT"]) &&
      redshift_grant.operator_languages.scope == "LANGUAGES" && redshift_grant.operator_languages.schema_name == null &&
      redshift_grant.loader_copy_jobs.scope == "COPY JOBS" && redshift_grant.loader_copy_jobs.user == redshift_user.loader.name &&
      redshift_grant.reader_templates.scope == "TEMPLATES" && redshift_grant.reader_templates.schema_name == redshift_schema.local.name
    )
    error_message = "User recipients with grant options and the LANGUAGES, COPY JOBS, and TEMPLATES scopes must be wired."
  }

  assert {
    condition = (
      redshift_object_grant.operators_public_tables.object_type == "ALL TABLES" && redshift_object_grant.operators_public_tables.object_name == null &&
      redshift_object_grant.reader_events_option.grant_option_privileges == toset(["SELECT"]) &&
      redshift_default_privileges.loader_functions_public.grantee_type == "PUBLIC" &&
      length(redshift_default_privileges.loader_functions_public.privileges) == 0 &&
      redshift_default_privileges.loader_tables_reader_option.grant_option_privileges == toset(["SELECT"]) &&
      length(redshift_assumerole_grant.operators_any_role) == 1 && redshift_assumerole_grant.operators_any_role[0].iam_role_arn == "ALL"
    )
    error_message = "Schema snapshots, object and default grant options, the implicit PUBLIC EXECUTE revocation, and ON ALL roles must be wired."
  }

  assert {
    condition = (
      output.grant_checks.loader_tables == toset(["INSERT", "SELECT"]) &&
      output.grant_checks.loader_table_options == toset(["SELECT"]) &&
      output.grant_checks.operators_snapshot == toset(["SELECT"]) &&
      length(output.grant_checks.public_function_execs) == 0
    )
    error_message = "The grant lookups must expose user grant options, snapshot privileges, and the PUBLIC function default."
  }
}
