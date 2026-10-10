# Mocked runs for masking policies, attachments, and policy grants. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed catalog values deliberately differ from the configuration where Redshift renders its own text.
override_data {
  target = data.redshift_masking_policy.email
  values = { expression = "CASE WHEN (\"masked_table\".\"email\" IN (SELECT ...)) THEN ... END" }
}
override_data {
  target = data.redshift_masking_policy_attachment.email_readers
  values = { exists = true, priority = 10, input_columns = ["email"] }
}

run "masking_apply" {
  command = apply

  assert {
    condition = (
      redshift_masking_policy.email.database == redshift_database.local.name &&
      length(redshift_masking_policy.email.input_column) == 1 &&
      redshift_masking_policy.email.input_column[0].type == "VARCHAR(256)" &&
      strcontains(redshift_masking_policy.email.expression, "public.${local.masking_lookup_name}")
    )
    error_message = "The masking policy must live in the consumer-local database and read its lookup table."
  }

  assert {
    condition = (
      redshift_policy_grant.masking_lookup.policy_type == "MASKING" &&
      redshift_policy_grant.masking_lookup.policy_name == redshift_masking_policy.email.name &&
      redshift_policy_grant.masking_lookup.object_name == local.masking_lookup_name &&
      redshift_policy_grant.masking_lookup.privileges == toset(["SELECT"])
    )
    error_message = "The policy must hold SELECT on its own lookup table."
  }

  assert {
    condition = (
      redshift_masking_policy_attachment.email_readers.policy == redshift_masking_policy.email.name &&
      redshift_masking_policy_attachment.email_readers.relation == local.masking_table_name &&
      redshift_masking_policy_attachment.email_readers.columns == tolist(["email"]) &&
      redshift_masking_policy_attachment.email_readers.grantee == redshift_role.readers.name &&
      redshift_masking_policy_attachment.email_readers.grantee_type == "ROLE" &&
      redshift_masking_policy_attachment.email_readers.priority == 10
    )
    error_message = "The attachment must mask the fixture's email column for the readers role."
  }

  assert {
    condition = (
      output.masking_checks.attached && output.masking_checks.attached_priority == 10 &&
      startswith(output.masking_checks.policy_expression, "CASE WHEN (\"masked_table\"") &&
      data.redshift_masking_policy.email.name == redshift_masking_policy.email.name &&
      data.redshift_masking_policies.local.database == redshift_database.local.name
    )
    error_message = "The masking lookups must be exposed through masking_checks."
  }
}
