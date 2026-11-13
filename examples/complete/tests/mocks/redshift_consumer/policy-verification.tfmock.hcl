# Targeted catalog observations avoid sharing incompatible table/grant defaults with other example features.
mock_resource "redshift_rls_policy" {
  defaults = { definition_fingerprint = "observed-fingerprint", alias = null }
}
mock_resource "redshift_masking_policy" {
  defaults = { definition_fingerprint = "observed-mask-fingerprint" }
}

override_data {
  target = data.redshift_rls_policy.own_region
  values = {
    alias                  = null
    predicate              = "\"region\" IN (SELECT \"region\" FROM example_schema.example_rls_regions WHERE \"reader\" = CAST(current_user AS TEXT))"
    definition_fingerprint = "observed-fingerprint"
  }
}
override_data {
  target = data.redshift_rls_policy_attachment.readers
  values = { exists = true }
}
override_data {
  target = data.redshift_table_security.rls_events
  values = { row_level_security = true, conjunction_type = "AND", datashare_row_level_security = true }
}
override_data {
  target = data.redshift_table.rls_events
  values = { owner = "admin" }
}
override_data {
  target = data.redshift_table.rls_regions
  values = { owner = "admin" }
}
override_data {
  target = data.redshift_masking_policy.email
  values = {
    expression             = "CASE WHEN (\"masked_table\".\"email\" IN (SELECT ...)) THEN ... END"
    definition_fingerprint = "observed-mask-fingerprint"
  }
}
override_data {
  target = data.redshift_masking_policy_attachment.email_readers
  values = { exists = true, priority = 10, input_columns = ["email"] }
}
override_data {
  target = data.redshift_table.customers
  values = { owner = "admin" }
}
override_data {
  target = data.redshift_table.masking_exempt
  values = { owner = "admin" }
}
override_data {
  target = data.redshift_user.model_delegate
  values = { superuser = false, create_database = false }
}
override_data {
  target = data.redshift_external_model.summarizer
  values = {
    function      = "f_example_summarize"
    iam_role      = "DEFAULT"
    model_id      = "amazon.nova-lite-v1:0"
    prompt        = "Summarize the following text in one sentence:"
    request_type  = "UNIFIED"
    response_type = "VARCHAR"
  }
}
override_data {
  target = data.redshift_object_grant.readers_summarizer
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.model_delegate_schema
  values = { privileges = ["USAGE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.delegate_summarizer
  values = { privileges = ["EXECUTE"], grant_option_privileges = ["EXECUTE"] }
}
override_data {
  target = data.redshift_object_grant.group_summarizer
  values = { privileges = ["EXECUTE"], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_object_grant.public_summarizer
  values = { privileges = [], grant_option_privileges = [] }
}
override_data {
  target = data.redshift_debug_grant.account_admins
  values = { exists = true }
}
override_data {
  target = data.redshift_role.sso_readers
  values = { role_id = 101, owner = "admin" }
}

override_data {
  target = data.redshift_identity_provider.this
  values = {
    type            = "AWSIDC"
    enabled         = true
    namespace       = "redshift_oss_e2e_deadbeef"
    application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef"
    iam_role_arn    = "arn:aws:iam::111111111111:role/consumer-fixture"
  }
}
override_data {
  target = data.redshift_role.sso_operators
  values = { role_id = 102, owner = "admin" }
}
override_data {
  target = data.redshift_role_grant.sso_readers
  values = { exists = true, admin_option = false }
}
override_data {
  target = data.redshift_role_grant.sso_operators
  values = { exists = true, admin_option = false }
}

override_resource {
  target = redshift_table.rls_events
  values = { owner = "admin" }
}
override_resource {
  target = redshift_table.rls_regions
  values = { owner = "admin" }
}
override_resource {
  target = redshift_table.customers
  values = { owner = "admin" }
}
override_resource {
  target = redshift_table.masking_exempt
  values = { owner = "admin" }
}
