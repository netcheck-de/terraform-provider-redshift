# Mocked runs for roles, role grants, and identity providers. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

override_data {
  target = data.redshift_roles.all
  values = { id = "roles-listing-identity" }
}
override_data {
  target = data.redshift_role_grants.operators
  values = { id = "operator-role-grants-listing-identity" }
}

run "roles_apply" {
  command = apply
  assert {
    condition = (
      redshift_role.auditors.name == "example_auditors" && redshift_role.auditors.owner == var.admin_username &&
      redshift_role_grant.loader_operators.role == redshift_role.operators.name &&
      redshift_role_grant.loader_operators.to_user == redshift_user.loader.name &&
      redshift_role_grant.loader_operators.to_role == null && redshift_role_grant.loader_operators.admin_option
    )
    error_message = "The audit role must belong to the administrator, and the loader must hold the operators role with the admin option."
  }
  assert {
    condition = (
      data.redshift_role.auditors.name == redshift_role.auditors.name &&
      data.redshift_role_grant.loader_operators.role == redshift_role_grant.loader_operators.role &&
      data.redshift_role_grant.loader_operators.to_user == redshift_role_grant.loader_operators.to_user &&
      output.role_details.readers.owner == data.redshift_role.readers.owner &&
      output.role_details.readers.id == data.redshift_role.readers.role_id &&
      output.role_details.auditors.owner == data.redshift_role.auditors.owner &&
      output.role_details.loader_operators.admin_option == data.redshift_role_grant.loader_operators.admin_option &&
      output.identity_provider_details == null
    )
    error_message = "Role lookups must follow the managed roles and grants, and outputs must expose their observations."
  }
  assert {
    condition = (
      data.redshift_roles.all.id == "roles-listing-identity" &&
      data.redshift_role_grants.operators.role == redshift_role.operators.name &&
      data.redshift_role_grants.operators.to_user == null && data.redshift_role_grants.operators.to_role == null &&
      output.role_listings.roles == [for role in data.redshift_roles.all.roles : role.name] &&
      output.role_listings.identity_providers == null
    )
    error_message = "Role and role grant listings must reach role_listings, without identity providers when SSO is disabled."
  }
}

run "wrong_role_owner_blocks_apply" {
  command   = apply
  state_key = "wrong_role_owner"
  override_data {
    target = data.redshift_role.auditors
    values = { role_id = 103, owner = "example_loader" }
  }
  expect_failures = [data.redshift_role.auditors]
}

run "invalid_operator_role_blocks_apply" {
  command   = apply
  state_key = "invalid_operator_role"
  override_data {
    target = data.redshift_role.operators
    values = { role_id = 0, owner = "tfadmin" }
  }
  expect_failures = [data.redshift_role.operators]
}

run "missing_role_inheritance_blocks_apply" {
  command   = apply
  state_key = "missing_role_inheritance"
  override_data {
    target = data.redshift_role_grant.operators
    values = { exists = false, admin_option = null }
  }
  expect_failures = [data.redshift_role_grant.operators]
}

run "reader_unexpected_admin_option_blocks_apply" {
  command   = apply
  state_key = "reader_unexpected_admin_option"
  override_data {
    target = data.redshift_role_grant.reader
    values = { exists = true, admin_option = true }
  }
  expect_failures = [data.redshift_role_grant.reader]
}

run "loader_missing_admin_option_blocks_apply" {
  command   = apply
  state_key = "loader_missing_admin_option"
  override_data {
    target = data.redshift_role_grant.loader_operators
    values = { exists = true, admin_option = false }
  }
  expect_failures = [data.redshift_role_grant.loader_operators]
}

run "roles_identity_center" {
  command   = apply
  state_key = "roles_identity_center"
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }
  override_data {
    target = data.redshift_identity_provider.this[0]
    values = {
      type                         = "AWSIDC"
      provider_id                  = 126692
      identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    }
  }
  override_data {
    target = data.redshift_identity_providers.all[0]
    values = { id = "identity-providers-listing-identity" }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["sso_oauth"]
    values = { service_name = "com.amazonaws.eu-central-1.sso-oauth" }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["identitystore"]
    values = { service_name = "com.amazonaws.eu-central-1.identitystore" }
  }
  assert {
    condition = (
      redshift_identity_provider.this[0].type == "AWSIDC" && redshift_identity_provider.this[0].auto_create_roles == false &&
      redshift_identity_provider.this[0].auto_create_roles_include_groups == null &&
      output.identity_provider_details.type == "AWSIDC" && output.identity_provider_details.provider_id == 126692 &&
      output.identity_provider_details.identity_center_instance_arn == var.identity_center_instance_arn
    )
    error_message = "The Identity Center provider must leave group roles to Terraform, and its lookup details must reach the outputs."
  }
  assert {
    condition = (
      data.redshift_identity_providers.all[0].id == "identity-providers-listing-identity" &&
      output.role_listings.identity_providers == [for provider in data.redshift_identity_providers.all[0].identity_providers : provider.name]
    )
    error_message = "The identity provider listing must reach role_listings when SSO is enabled."
  }
}
