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
      type                         = "awsidc"
      provider_id                  = 126692
      identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    }
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
      redshift_identity_provider.this[0].type == "awsidc" && redshift_identity_provider.this[0].auto_create_roles == false &&
      redshift_identity_provider.this[0].auto_create_roles_include_groups == null &&
      output.identity_provider_details.type == "awsidc" && output.identity_provider_details.provider_id == 126692 &&
      output.identity_provider_details.identity_center_instance_arn == var.identity_center_instance_arn
    )
    error_message = "The Identity Center provider must leave group roles to Terraform, and its lookup details must reach the outputs."
  }
}
