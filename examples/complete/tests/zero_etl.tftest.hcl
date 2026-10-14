# Mocked runs for the optional zero-ETL integration. The mocks are shared with composition.tftest.hcl through
# tests/mocks, so this block adds runs and assertions here without editing the composition suite.
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

run "zero_etl_disabled" {
  command = apply

  assert {
    condition = (
      length(aws_rds_integration.zero_etl) == 0 && length(aws_redshift_resource_policy.zero_etl) == 0 &&
      length(redshift_database.zero_etl) == 0 && output.zero_etl_database == null
    )
    error_message = "Without zero_etl_source_arn the example must not create a zero-ETL integration or destination."
  }

  assert {
    condition = !contains(
      [for parameter in aws_redshiftserverless_workgroup.consumer.config_parameter : parameter.parameter_key],
      "enable_case_sensitive_identifier",
    )
    error_message = "Case-sensitive identifiers must stay off without the zero-ETL integration."
  }
}

run "zero_etl_apply" {
  command = apply

  variables {
    zero_etl_source_arn      = "arn:aws:rds:eu-central-1:111111111111:cluster:orders"
    zero_etl_source_database = "orders"
  }

  override_resource {
    target = aws_rds_integration.zero_etl[0]
    values = { arn = "arn:aws:rds:eu-central-1:111111111111:integration:a1b2c3d4-5678-90ab-cdef-example11111" }
  }

  override_data {
    target = data.redshift_database.zero_etl[0]
    values = {
      id            = "zero-etl-database-lookup-identity"
      database_type = "INTEGRATION"
      owner         = "admin"
      integration = {
        integration_id   = "a1b2c3d4-5678-90ab-cdef-example11111"
        source_database  = "orders"
        refresh_interval = 600
        history_mode     = false
        auto_remediation = false
        # No catalog view reports these, so the lookup leaves them null.
        query_all_states     = null
        accept_invalid_chars = null
        truncate_columns     = null
      }
    }
  }

  assert {
    condition = contains(
      [for parameter in aws_redshiftserverless_workgroup.consumer.config_parameter : "${parameter.parameter_key}=${parameter.parameter_value}"],
      "enable_case_sensitive_identifier=true",
    )
    error_message = "The consumer workgroup must turn on case-sensitive identifiers for the zero-ETL target."
  }

  assert {
    condition = (
      aws_rds_integration.zero_etl[0].source_arn == "arn:aws:rds:eu-central-1:111111111111:cluster:orders" &&
      aws_rds_integration.zero_etl[0].target_arn == aws_redshiftserverless_namespace.consumer.arn &&
      aws_redshift_resource_policy.zero_etl[0].resource_arn == aws_redshiftserverless_namespace.consumer.arn
    )
    error_message = "The integration must replicate the source into the consumer namespace that authorizes it."
  }

  assert {
    condition = (
      redshift_database.zero_etl[0].integration.integration_id == "a1b2c3d4-5678-90ab-cdef-example11111" &&
      redshift_database.zero_etl[0].integration.source_database == "orders" &&
      redshift_database.zero_etl[0].integration.refresh_interval == 600 &&
      redshift_database.zero_etl[0].integration.query_all_states
    )
    error_message = "The destination database must receive the integration by its SVV_INTEGRATION identifier."
  }

  assert {
    condition = (
      output.zero_etl_database.name == redshift_database.zero_etl[0].name &&
      output.zero_etl_database.database_type == "INTEGRATION" &&
      output.zero_etl_database.integration_id == redshift_database.zero_etl[0].integration.integration_id &&
      output.zero_etl_database.refresh_interval == 600
    )
    error_message = "The zero-ETL output must expose the destination lookup."
  }
}
