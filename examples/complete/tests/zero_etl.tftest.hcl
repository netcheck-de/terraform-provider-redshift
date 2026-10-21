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
      length(aws_rds_integration.zero_etl) == 0 && length(aws_redshift_integration.zero_etl) == 0 &&
      length(aws_dynamodb_table.zero_etl_source) == 0 && length(aws_redshift_resource_policy.inbound_integrations) == 0 &&
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

run "zero_etl_dynamodb_existing" {
  command = apply

  variables {
    zero_etl_source_arn = "arn:aws:dynamodb:eu-central-1:111111111111:table/orders"
  }

  override_resource {
    target = aws_redshift_integration.zero_etl[0]
    values = { arn = "arn:aws:redshift:eu-central-1:111111111111:integration:a1b2c3d4-5678-90ab-cdef-example11111" }
  }

  override_data {
    target = data.redshift_database.zero_etl[0]
    values = {
      name          = "example_zero_etl"
      database_type = "LOCAL"
      owner         = "admin"
      integration = {
        integration_id       = "a1b2c3d4-5678-90ab-cdef-example11111"
        source_database      = null
        refresh_interval     = 900
        history_mode         = false
        auto_remediation     = false
        query_all_states     = null
        accept_invalid_chars = null
        truncate_columns     = null
      }
    }
  }

  assert {
    condition = (
      length(aws_rds_integration.zero_etl) == 0 && length(aws_dynamodb_table.zero_etl_source) == 0 &&
      length(aws_dynamodb_resource_policy.zero_etl_source) == 0 &&
      aws_redshift_integration.zero_etl[0].source_arn == var.zero_etl_source_arn &&
      aws_redshift_integration.zero_etl[0].target_arn == aws_redshiftserverless_namespace.consumer.arn &&
      contains(jsondecode(aws_redshift_resource_policy.inbound_integrations[0].policy).Statement[0].Condition.StringEquals["aws:SourceArn"], var.zero_etl_source_arn)
    )
    error_message = "An existing DynamoDB source must use the Redshift API and namespace policy without managing the source table or its policy."
  }

  assert {
    condition = (
      redshift_database.zero_etl[0].integration.integration_id == "a1b2c3d4-5678-90ab-cdef-example11111" &&
      redshift_database.zero_etl[0].integration.refresh_interval == 900 &&
      local.zero_etl_source_database == null &&
      output.zero_etl_database.source_database == null && output.zero_etl_database.refresh_interval == 900 &&
      output.zero_etl_database.integration_arn == aws_redshift_integration.zero_etl[0].arn &&
      contains([for parameter in aws_redshiftserverless_workgroup.consumer.config_parameter : "${parameter.parameter_key}=${parameter.parameter_value}"], "enable_case_sensitive_identifier=true")
    )
    error_message = "DynamoDB must omit DATABASE, use a supported interval, enable case-sensitive identifiers, and expose the Redshift integration ARN."
  }
}

run "zero_etl_dynamodb_created" {
  command = apply

  variables {
    zero_etl_create_dynamodb_source = true
  }

  override_resource {
    target = aws_dynamodb_table.zero_etl_source[0]
    values = { arn = "arn:aws:dynamodb:eu-central-1:111111111111:table/example-zero-etl-source" }
  }

  assert {
    condition = (
      length(aws_rds_cluster.zero_etl_source) == 0 && length(aws_rds_integration.zero_etl) == 0 &&
      aws_dynamodb_table.zero_etl_source[0].billing_mode == "PAY_PER_REQUEST" &&
      one(aws_dynamodb_table.zero_etl_source[0].point_in_time_recovery).enabled &&
      aws_redshift_integration.zero_etl[0].source_arn == aws_dynamodb_table.zero_etl_source[0].arn &&
      aws_dynamodb_resource_policy.zero_etl_source[0].resource_arn == aws_dynamodb_table.zero_etl_source[0].arn &&
      redshift_database.zero_etl[0].integration.refresh_interval == 900
    )
    error_message = "The disposable DynamoDB source must enable PITR and use a Redshift integration with a supported interval."
  }

  assert {
    condition = (
      jsondecode(aws_dynamodb_resource_policy.zero_etl_source[0].policy).Statement[0].Principal.Service == "redshift.amazonaws.com" &&
      toset(jsondecode(aws_dynamodb_resource_policy.zero_etl_source[0].policy).Statement[0].Action) == toset(["dynamodb:DescribeTable", "dynamodb:ExportTableToPointInTime"]) &&
      jsondecode(aws_dynamodb_resource_policy.zero_etl_source[0].policy).Statement[1].Action == "dynamodb:DescribeExport" &&
      jsondecode(aws_dynamodb_resource_policy.zero_etl_source[0].policy).Statement[1].Resource == "${aws_dynamodb_table.zero_etl_source[0].arn}/export/*" &&
      jsondecode(aws_dynamodb_resource_policy.zero_etl_source[0].policy).Statement[0].Condition.StringEquals["aws:SourceAccount"] == data.aws_caller_identity.consumer.account_id
    )
    error_message = "The source policy must authorize Redshift table exports and export-status reads, scoped to the consumer account."
  }
}

run "zero_etl_dynamodb_source_conflict" {
  command = plan

  variables {
    zero_etl_create_dynamodb_source = true
    zero_etl_source_arn             = "arn:aws:dynamodb:eu-central-1:111111111111:table/orders"
  }

  expect_failures = [var.zero_etl_create_dynamodb_source]
}

run "zero_etl_created_source_kind_conflict" {
  command = plan

  variables {
    zero_etl_create_source          = true
    zero_etl_create_dynamodb_source = true
  }

  expect_failures = [var.zero_etl_create_source]
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
      database_type = "LOCAL"
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
      aws_redshift_resource_policy.inbound_integrations[0].resource_arn == aws_redshiftserverless_namespace.consumer.arn
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
      output.zero_etl_database.database_type == "LOCAL" &&
      output.zero_etl_database.integration_id == redshift_database.zero_etl[0].integration.integration_id &&
      output.zero_etl_database.refresh_interval == 600 &&
      output.zero_etl_database.integration_arn == aws_rds_integration.zero_etl[0].arn
    )
    error_message = "The zero-ETL output must expose the destination lookup."
  }
}

run "zero_etl_created_source" {
  command = apply

  variables {
    zero_etl_create_source = true
  }

  override_resource {
    target = aws_rds_cluster.zero_etl_source[0]
    values = { arn = "arn:aws:rds:eu-central-1:111111111111:cluster:example-zero-etl-source" }
  }

  assert {
    condition = (
      aws_rds_cluster.zero_etl_source[0].engine == "aurora-postgresql" &&
      aws_rds_cluster.zero_etl_source[0].db_cluster_parameter_group_name == aws_rds_cluster_parameter_group.zero_etl_source[0].name &&
      aws_rds_cluster_instance.zero_etl_source[0].instance_class == "db.serverless" &&
      length(setsubtract(
        ["rds.logical_replication=1", "aurora.enhanced_logical_replication=1", "aurora.logical_replication_backup=0", "aurora.logical_replication_globaldb=0"],
        [for parameter in aws_rds_cluster_parameter_group.zero_etl_source[0].parameter : "${parameter.name}=${parameter.value}"],
      )) == 0
    )
    error_message = "The created source must be Aurora PostgreSQL Serverless v2 with enhanced logical replication."
  }

  assert {
    condition = (
      aws_rds_integration.zero_etl[0].source_arn == aws_rds_cluster.zero_etl_source[0].arn &&
      aws_redshift_resource_policy.inbound_integrations[0].resource_arn == aws_redshiftserverless_namespace.consumer.arn &&
      redshift_database.zero_etl[0].integration.source_database == aws_rds_cluster.zero_etl_source[0].database_name
    )
    error_message = "The destination must replicate the created source's database through the integration."
  }
}

run "zero_etl_source_conflict" {
  command = plan

  variables {
    zero_etl_create_source = true
    zero_etl_source_arn    = "arn:aws:rds:eu-central-1:111111111111:cluster:orders"
  }

  expect_failures = [var.zero_etl_create_source]
}
