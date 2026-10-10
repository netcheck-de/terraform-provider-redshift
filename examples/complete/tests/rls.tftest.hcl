# Mocked runs for row-level security policies, attachments, and table security. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed catalog values deliberately differ from configuration where Redshift rewrites them. Terraform cannot
# override computed lists of nested objects (columns, items), so those keep their mocked values.
override_data {
  target = data.redshift_rls_policy.own_region
  values = {
    id                     = "rls-policy-lookup-identity"
    predicate              = "\"region\" = CAST(current_user AS TEXT)"
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

run "rls_apply" {
  command = apply

  assert {
    condition = (
      redshift_rls_policy.own_region.database == redshift_database.local.name &&
      redshift_rls_policy.own_region.predicate == "region = current_user" &&
      redshift_rls_policy.own_region.columns[0].name == "region" &&
      redshift_rls_policy_attachment.readers.policy == redshift_rls_policy.own_region.name &&
      redshift_rls_policy_attachment.readers.relation == local.rls_table_name &&
      redshift_rls_policy_attachment.readers.schema == "public" &&
      redshift_rls_policy_attachment.readers.grantee == redshift_role.readers.name &&
      redshift_rls_policy_attachment.readers.grantee_type == "ROLE" &&
      aws_redshiftdata_statement.rls_table.database == redshift_database.local.name &&
      strcontains(aws_redshiftdata_statement.rls_table.sql, "public.${local.rls_table_name} (id INTEGER, region VARCHAR(64))")
    )
    error_message = "The policy must filter its own fixture table in the owned local database and be attached to the reader role."
  }
  assert {
    condition = (
      redshift_table_security.rls_events.database == redshift_rls_policy_attachment.readers.database &&
      redshift_table_security.rls_events.schema == redshift_rls_policy_attachment.readers.schema &&
      redshift_table_security.rls_events.relation == redshift_rls_policy_attachment.readers.relation &&
      redshift_table_security.rls_events.row_level_security &&
      redshift_table_security.rls_events.conjunction_type == "AND" &&
      redshift_table_security.rls_events.datashare_row_level_security
    )
    error_message = "Row-level security must be turned on for exactly the attached fixture table and stay on for datashares."
  }
  assert {
    condition = (
      output.row_level_security.policy.id == "rls-policy-lookup-identity" &&
      output.row_level_security.policy.predicate == "\"region\" = CAST(current_user AS TEXT)" &&
      output.row_level_security.policy.fingerprint == "observed-fingerprint" &&
      output.row_level_security.attached &&
      output.row_level_security.table.row_level_security &&
      output.row_level_security.table.conjunction_type == "AND" &&
      output.row_level_security.table.datashare_row_level_security &&
      can(length(output.row_level_security.policies))
    )
    error_message = "Row-level security lookups must be exposed from the catalog observations, not from configuration."
  }
}
