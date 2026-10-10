# Mocked runs for datashares, datashare grants, and datashare privileges. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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

# Observed catalog values that the outputs must pass through unchanged.
override_data {
  target = data.redshift_datashare.producer
  values = {
    publicly_accessible = false
    owner               = "observed_share_owner"
    share_id            = 100
    producer_namespace  = "11111111-2222-3333-4444-555555555555"
  }
}
override_data {
  target = data.redshift_datashare_privilege.share_operators
  values = { privileges = ["ALTER", "SHARE"] }
}
# Terraform test mocks cannot override list-nested attributes such as a listing's elements, so the listing runs
# assert the filters and the output wiring instead of element values.
override_data {
  target = data.redshift_datashares.outbound
  values = { id = "outbound-listing-identity" }
}
override_data {
  target = data.redshift_datashares.inbound
  values = { id = "inbound-listing-identity" }
}

run "datasharing_apply" {
  command = apply

  assert {
    condition = (
      redshift_role.share_operators.name == "example_share_operators" &&
      redshift_datashare_privilege.share_operators.database_name == redshift_datashare.grants.database &&
      redshift_datashare_privilege.share_operators.datashare_name == redshift_datashare.grants.name &&
      redshift_datashare_privilege.share_operators.grantee_type == "ROLE" &&
      redshift_datashare_privilege.share_operators.grantee == redshift_role.share_operators.name &&
      redshift_datashare_privilege.share_operators.privileges == toset(["ALTER", "SHARE"]) &&
      data.redshift_datashare_privilege.share_operators.datashare_name == redshift_datashare.grants.name &&
      data.redshift_datashare_privilege.share_operators.grantee == redshift_role.share_operators.name
    )
    error_message = "The share operator role must hold exactly ALTER and SHARE on the grants share, observed by the lookup."
  }
  assert {
    condition = (
      length(redshift_datashare_grant.lake_formation) == 0 && length(aws_redshift_data_share_authorization.lake_formation) == 0 &&
      data.redshift_datashares.outbound.share_type == "OUTBOUND" && data.redshift_datashares.outbound.name == null &&
      data.redshift_datashares.inbound.share_type == "INBOUND" && data.redshift_datashares.inbound.name == redshift_datashare.producer.name
    )
    error_message = "Without a Lake Formation account no Data Catalog grant exists, and the listings filter each side of the share."
  }
  assert {
    condition = (
      output.datasharing.producer_owner == "observed_share_owner" && output.datasharing.producer_share_id == 100 &&
      output.datasharing.producer_namespace == "11111111-2222-3333-4444-555555555555" &&
      output.datasharing.share_operator_privileges == toset(["ALTER", "SHARE"]) &&
      data.redshift_datashares.outbound.id == "outbound-listing-identity" && data.redshift_datashares.inbound.id == "inbound-listing-identity" &&
      output.datasharing.outbound_shares == [for share in data.redshift_datashares.outbound.datashares : share.name] &&
      length(output.datasharing.inbound_shares) == length(data.redshift_datashares.inbound.datashares)
    )
    error_message = "The datasharing output must pass the observed share metadata, listings, and permissions through unchanged."
  }
}

run "datasharing_lake_formation" {
  command = apply

  variables {
    lake_formation_account_id = "333333333333"
  }

  assert {
    condition = (
      length(redshift_datashare_grant.lake_formation) == 1 &&
      redshift_datashare_grant.lake_formation[0].via_data_catalog &&
      redshift_datashare_grant.lake_formation[0].account_id == "333333333333" &&
      redshift_datashare_grant.lake_formation[0].namespace_id == null &&
      redshift_datashare_grant.lake_formation[0].datashare == redshift_datashare.grants.name &&
      aws_redshift_data_share_authorization.lake_formation[0].consumer_identifier == "DataCatalog/333333333333" &&
      aws_redshift_data_share_authorization.lake_formation[0].data_share_arn == "arn:aws:redshift:eu-central-1:111111111111:datashare:11111111-2222-3333-4444-555555555555/example_share_grants"
    )
    error_message = "A Lake Formation account must receive usage VIA DATA CATALOG on the grants share and the matching DataCatalog authorization."
  }
}

run "datasharing_rejects_invalid_lake_formation_account" {
  command = plan

  variables {
    lake_formation_account_id = "not-an-account"
  }

  expect_failures = [var.lake_formation_account_id]
}
