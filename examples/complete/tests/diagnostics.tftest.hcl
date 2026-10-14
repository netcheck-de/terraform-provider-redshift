# Mocked runs for configuration and lockout diagnostics and the optional DEBUG grant. The mocks are shared with
# composition.tftest.hcl through tests/mocks, so this block adds runs and assertions here without editing the
# composition suite.
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
  target = data.redshift_configuration.case_sensitive_identifiers
  values = { value = "off" }
}
override_data {
  target = data.redshift_configuration.search_path
  values = { value = "$user, public" }
}
override_data {
  target = data.redshift_debug_grant.account_admins
  values = { exists = true }
}

run "diagnostics_default" {
  command = apply

  assert {
    condition = (
      data.redshift_configuration.case_sensitive_identifiers.name == "enable_case_sensitive_identifier" &&
      data.redshift_configuration.search_path.name == "search_path" &&
      output.diagnostics.case_sensitive_identifiers == "off" && output.diagnostics.search_path == "$user, public"
    )
    error_message = "The configuration lookups must read the consumer session's parameters into the output."
  }
  assert {
    condition     = length(data.redshift_user_lockouts.reader) == 0 && output.diagnostics.reader_lockouts == null
    error_message = "Without user_lockouts the lockout listing must be skipped."
  }
  assert {
    condition     = length(redshift_debug_grant.account_admins) == 0 && output.diagnostics.account_admins_debug == null
    error_message = "Without federated_debug_database the DEBUG grant and its lookup must be skipped."
  }
}

run "diagnostics_debug_grant" {
  command = apply

  variables {
    federated_debug_database = "sales_db@finance-catalog"
  }

  assert {
    condition = (
      redshift_debug_grant.account_admins[0].database == "sales_db@finance-catalog" &&
      redshift_debug_grant.account_admins[0].grantee == "111111111111" &&
      redshift_debug_grant.account_admins[0].grantee_type == "ACCOUNT"
    )
    error_message = "The DEBUG grant must go to the administrators of the consumer account on the configured database."
  }
  assert {
    condition = (
      data.redshift_debug_grant.account_admins[0].database == redshift_debug_grant.account_admins[0].database &&
      data.redshift_debug_grant.account_admins[0].grantee == redshift_debug_grant.account_admins[0].grantee &&
      output.diagnostics.account_admins_debug == true
    )
    error_message = "The DEBUG lookup must check the managed grant and expose the result."
  }
}

run "diagnostics_user_lockouts" {
  command = apply

  variables {
    user_lockouts = true
  }

  # Terraform mocks generate empty lists for nested-attribute lists, so the lockout rows are covered by the Go tests;
  # here the listing must be narrowed to the reader and the output must keep its shape.
  assert {
    condition = (
      data.redshift_user_lockouts.reader[0].user_name == redshift_user.reader.name &&
      output.diagnostics.reader_lockouts == []
    )
    error_message = "The lockout listing must be narrowed to the example reader."
  }
}
