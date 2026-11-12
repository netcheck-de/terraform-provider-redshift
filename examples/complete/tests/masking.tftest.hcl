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

mock_provider "redshift" {
  alias  = "producer"
  source = "./tests/mocks/redshift_producer"
}
mock_provider "redshift" {
  alias  = "consumer"
  source = "./tests/mocks/redshift_consumer"
}
mock_provider "redshift" {
  alias  = "producer_data_api_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_data_api_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "producer_direct_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_direct_iam"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "producer_direct_password"
  source = "./tests/mocks/redshift_connections"
}
mock_provider "redshift" {
  alias  = "consumer_direct_password"
  source = "./tests/mocks/redshift_connections"
}

# Observed catalog values deliberately differ from the configuration where Redshift renders its own text.
override_data {
  target = data.redshift_masking_policy.email
  values = {
    expression             = "CASE WHEN (\"masked_table\".\"email\" IN (SELECT ...)) THEN ... END"
    definition_fingerprint = "observed-mask-fingerprint"
  }
}

run "masking_missing_attachment_blocks" {
  command = apply

  override_data {
    target = data.redshift_masking_policy_attachment.email_readers
    values = { exists = false, priority = 10, input_columns = ["email"] }
  }

  expect_failures = [data.redshift_masking_policy_attachment.email_readers]
}

run "masking_wrong_priority_blocks" {
  command = apply

  override_data {
    target = data.redshift_masking_policy_attachment.email_readers
    values = { exists = true, priority = 1, input_columns = ["email"] }
  }

  expect_failures = [data.redshift_masking_policy_attachment.email_readers]
}

run "masking_changed_expression_blocks" {
  command = apply

  override_data {
    target = data.redshift_masking_policy.email
    values = {
      definition_fingerprint = "different-catalog-expression"
    }
  }

  expect_failures = [data.redshift_masking_policy.email]
}

run "masking_wrong_table_owner_blocks" {
  command = apply

  override_data {
    target = data.redshift_table.customers
    values = { owner = "unexpected_owner" }
  }

  expect_failures = [data.redshift_table.customers]
}
override_data {
  target = data.redshift_masking_policy_attachment.email_readers
  values = { exists = true, priority = 10, input_columns = ["email"] }
}
override_data {
  target = data.redshift_masking_policy_attachments.customers
  values = { id = "masking-attachments-listing-identity" }
}

run "masking_apply" {
  command = apply

  assert {
    condition = (
      redshift_masking_policy.email.database == redshift_database.local.name &&
      length(redshift_masking_policy.email.input_column) == 1 &&
      redshift_masking_policy.email.input_column[0].type == "VARCHAR(256)" &&
      strcontains(redshift_masking_policy.email.expression, "${redshift_schema.local.name}.example_masking_exempt")
    )
    error_message = "The masking policy must live in the consumer-local database and read its lookup table."
  }

  assert {
    condition = (
      redshift_policy_grant.masking_lookup.policy_type == "MASKING" &&
      redshift_policy_grant.masking_lookup.policy_name == redshift_masking_policy.email.name &&
      redshift_policy_grant.masking_lookup.schema_name == redshift_table.masking_exempt.schema &&
      redshift_policy_grant.masking_lookup.object_name == redshift_table.masking_exempt.name &&
      redshift_policy_grant.masking_lookup.privileges == toset(["SELECT"])
    )
    error_message = "The policy must hold SELECT on its own lookup table."
  }

  assert {
    condition = (
      redshift_masking_policy_attachment.email_readers.policy == redshift_masking_policy.email.name &&
      redshift_masking_policy_attachment.email_readers.schema == redshift_schema.local.name &&
      redshift_masking_policy_attachment.email_readers.relation == redshift_table.customers.name &&
      redshift_masking_policy_attachment.email_readers.columns == tolist(["email"]) &&
      redshift_masking_policy_attachment.email_readers.grantee == redshift_role.readers.name &&
      redshift_masking_policy_attachment.email_readers.grantee_type == "ROLE" &&
      redshift_masking_policy_attachment.email_readers.priority == 10
    )
    error_message = "The attachment must mask the managed customers table's email column for the readers role."
  }

  assert {
    condition = (
      redshift_table.customers.schema == redshift_schema.local.name && redshift_table.masking_exempt.schema == redshift_schema.local.name &&
      [for column in redshift_table.customers.column : column.name] == ["id", "email"] &&
      lower(redshift_table.customers.column[1].type) == lower(redshift_masking_policy.email.input_column[0].type)
    )
    error_message = "The masked table and the lookup table must be managed tables in the workspace schema."
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

  assert {
    condition = (
      data.redshift_masking_policy_attachments.customers.id == "masking-attachments-listing-identity" &&
      data.redshift_masking_policy_attachments.customers.database == redshift_database.local.name &&
      data.redshift_masking_policy_attachments.customers.schema == redshift_table.customers.schema &&
      data.redshift_masking_policy_attachments.customers.relation == redshift_table.customers.name &&
      data.redshift_masking_policy_attachments.customers.policy == null &&
      output.masking_checks.table_attachments == {
        for attachment in data.redshift_masking_policy_attachments.customers.masking_policy_attachments :
        "${attachment.policy} ${attachment.grantee_type} ${attachment.grantee}" => attachment.columns
      }
    )
    error_message = "The attachment listing must be scoped to the masked table and exposed through masking_checks."
  }
}
