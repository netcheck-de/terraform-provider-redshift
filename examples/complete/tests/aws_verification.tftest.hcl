# Offline service-readback contracts. Every failure changes an observed service response, not managed configuration.
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
mock_provider "redshift" { alias = "producer_data_api_iam" }
mock_provider "redshift" { alias = "consumer_data_api_iam" }
mock_provider "redshift" { alias = "producer_direct_iam" }
mock_provider "redshift" { alias = "consumer_direct_iam" }
mock_provider "redshift" { alias = "producer_direct_password" }
mock_provider "redshift" { alias = "consumer_direct_password" }

run "private_service_readbacks" {
  # Populate test-only state: a fresh plan otherwise defers service reads behind managed-resource creation.
  command = apply

  # Exercise this verification graph in isolation; SQL readback contracts have their own feature suites.
  plan_options {
    target = [
      data.aws_vpc.verify_producer, data.aws_vpc.verify_consumer,
      data.aws_subnet.verify_producer, data.aws_subnet.verify_consumer,
      data.aws_route_table.verify_producer, data.aws_route_table.verify_consumer,
      data.aws_vpc_endpoint.verify_producer, data.aws_vpc_endpoint.verify_consumer,
      data.aws_vpc_security_group_rules.verify_producer, data.aws_vpc_security_group_rules.verify_consumer,
      data.aws_vpc_security_group_rule.verify_producer_sql, data.aws_vpc_security_group_rule.verify_consumer_sql,
      data.aws_vpc_security_group_rule.verify_producer_egress, data.aws_vpc_security_group_rule.verify_consumer_egress,
      data.aws_redshift_cluster.verify_producer, data.aws_redshift_subnet_group.verify_producer,
      data.aws_redshiftserverless_namespace.verify_consumer, data.aws_redshiftserverless_workgroup.verify_consumer,
      data.aws_iam_role.verify_producer, data.aws_iam_role.verify_consumer, data.aws_iam_role.verify_lambda,
      data.aws_iam_role_policies.verify_producer, data.aws_iam_role_policies.verify_consumer, data.aws_iam_role_policy_attachments.verify_lambda,
      data.aws_s3_bucket.verify_fixture, data.aws_s3_bucket_policy.verify_fixture, data.aws_s3_object.verify_fixture,
      data.aws_glue_catalog_table.verify_fixture, data.aws_lambda_function.verify_upper,
      data.aws_secretsmanager_secret_version.verify_reader, data.aws_dynamodb_table.verify_zero_etl,
      data.aws_rds_cluster.verify_zero_etl, data.aws_db_instance.verify_zero_etl,
      data.aws_ssoadmin_application.verify_consumer, data.aws_ssoadmin_application_assignments.verify_consumer,
      data.aws_identitystore_group.verify_consumer,
    ]
  }

  assert {
    condition = (
      length(data.aws_subnet.verify_producer) == 3 && length(data.aws_subnet.verify_consumer) == 3 &&
      length(data.aws_route_table.verify_producer) == 3 && length(data.aws_route_table.verify_consumer) == 3 &&
      length(data.aws_vpc_security_group_rule.verify_producer_sql) == 0 &&
      length(data.aws_vpc_security_group_rule.verify_consumer_sql) == 0 &&
      length(data.aws_dynamodb_table.verify_zero_etl) == 1 && length(data.aws_rds_cluster.verify_zero_etl) == 0
    )
    error_message = "The fixture seed must execute both private network readbacks and the owned DynamoDB source lookup."
  }
}

run "private_service_readbacks_plan" {
  command = plan

  plan_options {
    target = [
      data.aws_vpc.verify_producer, data.aws_vpc.verify_consumer,
      data.aws_subnet.verify_producer, data.aws_subnet.verify_consumer,
      data.aws_route_table.verify_producer, data.aws_route_table.verify_consumer,
      data.aws_vpc_endpoint.verify_producer, data.aws_vpc_endpoint.verify_consumer,
      data.aws_vpc_security_group_rules.verify_producer, data.aws_vpc_security_group_rules.verify_consumer,
      data.aws_vpc_security_group_rule.verify_producer_egress, data.aws_vpc_security_group_rule.verify_consumer_egress,
      data.aws_redshift_cluster.verify_producer, data.aws_redshift_subnet_group.verify_producer,
      data.aws_redshiftserverless_namespace.verify_consumer, data.aws_redshiftserverless_workgroup.verify_consumer,
      data.aws_iam_role.verify_producer, data.aws_iam_role.verify_consumer, data.aws_iam_role.verify_lambda,
      data.aws_iam_role_policies.verify_producer, data.aws_iam_role_policies.verify_consumer, data.aws_iam_role_policy_attachments.verify_lambda,
      data.aws_s3_bucket.verify_fixture, data.aws_s3_bucket_policy.verify_fixture, data.aws_s3_object.verify_fixture,
      data.aws_glue_catalog_table.verify_fixture, data.aws_lambda_function.verify_upper,
      data.aws_secretsmanager_secret_version.verify_reader, data.aws_dynamodb_table.verify_zero_etl,
    ]
  }

  # Unlike instance-count assertions, observed-field assertions fail if Terraform defers the read until apply.
  assert {
    condition = (
      data.aws_vpc.verify_producer.cidr_block == "10.42.0.0/16" && data.aws_vpc.verify_consumer.cidr_block == "10.43.0.0/16" &&
      alltrue([for subnet in data.aws_subnet.verify_producer : subnet.vpc_id == "vpc-producer"]) &&
      alltrue([for subnet in data.aws_subnet.verify_consumer : subnet.vpc_id == "vpc-consumer"]) &&
      alltrue([for route_table in data.aws_route_table.verify_producer : route_table.vpc_id == "vpc-producer"]) &&
      alltrue([for route_table in data.aws_route_table.verify_consumer : route_table.vpc_id == "vpc-consumer"]) &&
      alltrue([for endpoint in data.aws_vpc_endpoint.verify_producer : endpoint.state == "available"]) &&
      alltrue([for endpoint in data.aws_vpc_endpoint.verify_consumer : endpoint.state == "available"]) &&
      data.aws_redshift_cluster.verify_producer.encrypted && !data.aws_redshiftserverless_workgroup.verify_consumer.publicly_accessible &&
      data.aws_redshiftserverless_namespace.verify_consumer.db_name == "dev" &&
      data.aws_iam_role.verify_producer.arn == "arn:aws:iam::111111111111:role/producer-fixture" &&
      data.aws_iam_role.verify_consumer.arn == "arn:aws:iam::111111111111:role/consumer-fixture" &&
      data.aws_lambda_function.verify_upper.runtime == "python3.12" &&
      data.aws_s3_object.verify_fixture.body == "id,label\n1,first\n2,second\n" &&
      data.aws_glue_catalog_table.verify_fixture.storage_descriptor[0].location == "s3://mock-fixture/data/" &&
      contains(data.aws_secretsmanager_secret_version.verify_reader.version_stages, "AWSCURRENT") &&
      data.aws_dynamodb_table.verify_zero_etl[0].point_in_time_recovery[0].enabled
    )
    error_message = "The no-change private plan must evaluate concrete AWS observations, not merely defer postconditions until apply."
  }
}

run "rejects_wrong_vpc_account" {
  command = plan
  plan_options { target = [data.aws_vpc.verify_producer] }

  override_data {
    target = data.aws_vpc.verify_producer
    values = { cidr_block = "10.42.0.0/16", owner_id = "222222222222", enable_dns_support = true, enable_dns_hostnames = true }
  }
  expect_failures = [data.aws_vpc.verify_producer]
}

run "rejects_wrong_subnet_network" {
  command = plan
  plan_options { target = [data.aws_subnet.verify_consumer] }

  override_data {
    target = data.aws_subnet.verify_consumer["1"]
    values = { vpc_id = "vpc-unrelated", cidr_block = "10.43.1.0/24", availability_zone = "eu-central-1b", owner_id = "111111111111", map_public_ip_on_launch = false }
  }
  expect_failures = [data.aws_subnet.verify_consumer["1"]]
}

run "rejects_private_internet_route" {
  command = plan
  plan_options { target = [data.aws_route_table.verify_producer] }

  override_data {
    target = data.aws_route_table.verify_producer["0"]
    values = {
      vpc_id = "vpc-producer"
      routes = [
        { cidr_block = "", gateway_id = "vpce-producer-s3", destination_prefix_list_id = "pl-producer-s3", ipv6_cidr_block = "" },
        { cidr_block = "0.0.0.0/0", gateway_id = "igw-unexpected", destination_prefix_list_id = "", ipv6_cidr_block = "" },
      ]
    }
  }
  expect_failures = [data.aws_route_table.verify_producer["0"]]
}

run "rejects_nondefault_private_escape_route" {
  command = plan
  plan_options { target = [data.aws_route_table.verify_consumer] }

  override_data {
    target = data.aws_route_table.verify_consumer["0"]
    values = {
      vpc_id = "vpc-consumer"
      routes = [
        { cidr_block = "10.43.0.0/16", gateway_id = "local", destination_prefix_list_id = "", ipv6_cidr_block = "" },
        { cidr_block = "", gateway_id = "vpce-consumer-s3", destination_prefix_list_id = "pl-consumer-s3", ipv6_cidr_block = "" },
        { cidr_block = "203.0.113.0/24", gateway_id = "igw-unexpected", destination_prefix_list_id = "", ipv6_cidr_block = "" },
      ]
    }
  }

  expect_failures = [data.aws_route_table.verify_consumer["0"]]
}

run "rejects_missing_private_endpoint_dns" {
  command = plan
  plan_options { target = [data.aws_vpc_endpoint.verify_consumer] }

  override_data {
    target = data.aws_vpc_endpoint.verify_consumer["glue"]
    values = {
      state               = "available", vpc_id = "vpc-consumer", service_name = "com.amazonaws.eu-central-1.glue", vpc_endpoint_type = "Interface"
      private_dns_enabled = false
      subnet_ids          = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
      security_group_ids  = ["sg-consumer-endpoints"]
    }
  }
  expect_failures = [data.aws_vpc_endpoint.verify_consumer["glue"]]
}

run "rejects_unmanaged_security_group_rule" {
  command = plan
  plan_options { target = [data.aws_vpc_security_group_rules.verify_consumer] }

  override_data {
    target = data.aws_vpc_security_group_rules.verify_consumer
    values = { ids = ["sgr-consumer-vpc", "sgr-consumer-s3", "sgr-unmanaged-ingress"] }
  }
  expect_failures = [data.aws_vpc_security_group_rules.verify_consumer]
}

run "rejects_unrestricted_egress" {
  command = plan
  plan_options { target = [data.aws_vpc_security_group_rule.verify_producer_egress] }

  override_data {
    target = data.aws_vpc_security_group_rule.verify_producer_egress["vpc"]
    values = { is_egress = true, security_group_id = "sg-producer", ip_protocol = "-1", cidr_ipv4 = "0.0.0.0/0" }
  }
  expect_failures = [data.aws_vpc_security_group_rule.verify_producer_egress["vpc"]]
}

run "rejects_workgroup_wrong_namespace" {
  command = plan
  plan_options { target = [data.aws_redshiftserverless_workgroup.verify_consumer] }

  override_data {
    target = data.aws_redshiftserverless_workgroup.verify_consumer
    values = {
      namespace_name     = "unrelated-namespace", enhanced_vpc_routing = true, publicly_accessible = false
      subnet_ids         = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
      security_group_ids = ["sg-consumer"]
      endpoint           = [{ address = "consumer.example.test", port = 5439 }]
    }
  }
  expect_failures = [data.aws_redshiftserverless_workgroup.verify_consumer]
}

run "rejects_unencrypted_producer" {
  command = plan
  plan_options { target = [data.aws_redshift_cluster.verify_producer] }

  # All other fields match the default observation so encryption alone triggers the failure.
  override_data {
    target = data.aws_redshift_cluster.verify_producer
    values = {
      encrypted                    = false, enhanced_vpc_routing = true, publicly_accessible = false, vpc_id = "vpc-producer"
      node_type                    = "ra3.large", database_name = "dev", master_username = "tfadmin", port = 5439
      cluster_subnet_group_name    = "redshift-oss-e2e-deadbeef-producer"
      cluster_parameter_group_name = "redshift-oss-e2e-deadbeef-producer"
      vpc_security_group_ids       = ["sg-producer"]
      iam_roles                    = ["arn:aws:iam::111111111111:role/producer-fixture"]
      default_iam_role_arn         = "arn:aws:iam::111111111111:role/producer-fixture"
      cluster_namespace_arn        = "arn:aws:redshift:eu-central-1:111111111111:namespace:11111111-2222-3333-4444-555555555555"
    }
  }
  expect_failures = [data.aws_redshift_cluster.verify_producer]
}

run "rejects_wrong_role_trust_account" {
  command = plan
  plan_options { target = [data.aws_iam_role.verify_producer] }

  override_data {
    target = data.aws_iam_role.verify_producer
    values = {
      arn = "arn:aws:iam::111111111111:role/producer-fixture"
      assume_role_policy = jsonencode({
        Statement = [{
          Effect    = "Allow", Principal = { Service = "redshift.amazonaws.com" }, Action = "sts:AssumeRole"
          Condition = { StringEquals = { "aws:SourceAccount" = "222222222222" } }
        }]
      })
    }
  }
  expect_failures = [data.aws_iam_role.verify_producer]
}

run "rejects_extra_trusted_principal" {
  command = plan
  plan_options { target = [data.aws_iam_role.verify_lambda] }

  override_data {
    target = data.aws_iam_role.verify_lambda
    values = {
      arn = "arn:aws:iam::111111111111:role/lambda-udf-exec"
      assume_role_policy = jsonencode({
        Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com", AWS = "*" }, Action = "sts:AssumeRole" }]
      })
    }
  }

  expect_failures = [data.aws_iam_role.verify_lambda]
}

run "rejects_missing_inline_policy" {
  command = plan
  plan_options { target = [data.aws_iam_role_policies.verify_producer] }

  override_data {
    target = data.aws_iam_role_policies.verify_producer
    values = { policy_names = ["fixture"] }
  }

  expect_failures = [data.aws_iam_role_policies.verify_producer]
}

run "rejects_overprivileged_lambda_managed_policy" {
  command = plan
  plan_options { target = [data.aws_iam_role_policy_attachments.verify_lambda] }

  override_data {
    target = data.aws_iam_role_policy_attachments.verify_lambda
    values = {
      attached_policies = [
        { policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole", policy_name = "AWSLambdaBasicExecutionRole" },
        { policy_arn = "arn:aws:iam::aws:policy/AdministratorAccess", policy_name = "AdministratorAccess" },
      ]
    }
  }

  expect_failures = [data.aws_iam_role_policy_attachments.verify_lambda]
}

run "rejects_missing_bucket_tls_deny" {
  command = plan
  plan_options { target = [data.aws_s3_bucket_policy.verify_fixture] }

  override_data {
    target = data.aws_s3_bucket_policy.verify_fixture
    values = { policy = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  expect_failures = [data.aws_s3_bucket_policy.verify_fixture]
}

run "rejects_wrong_fixture_rows" {
  command = plan
  plan_options { target = [data.aws_s3_object.verify_fixture] }

  override_data {
    target = data.aws_s3_object.verify_fixture
    values = { body = "id,label\n1,wrong\n2,second\n", content_type = "text/csv", server_side_encryption = "AES256" }
  }
  expect_failures = [data.aws_s3_object.verify_fixture]
}

run "rejects_wrong_glue_location" {
  command = plan
  plan_options { target = [data.aws_glue_catalog_table.verify_fixture] }

  override_data {
    target = data.aws_glue_catalog_table.verify_fixture
    values = {
      table_type = "EXTERNAL_TABLE", parameters = { "skip.header.line.count" = "1" }
      storage_descriptor = [{
        location    = "s3://unrelated/data/", input_format = "org.apache.hadoop.mapred.TextInputFormat", output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"
        ser_de_info = [{ serialization_library = "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe", parameters = { "field.delim" = "," } }]
        columns     = [{ name = "id", type = "int" }, { name = "label", type = "varchar(64)" }]
      }]
    }
  }
  expect_failures = [data.aws_glue_catalog_table.verify_fixture]
}

run "rejects_wrong_lambda_archive" {
  command = plan
  plan_options { target = [data.aws_lambda_function.verify_upper] }

  override_data {
    target = data.aws_lambda_function.verify_upper
    values = { role = "arn:aws:iam::111111111111:role/lambda-udf-exec", runtime = "python3.12", handler = "index.handler", code_sha256 = "wrong-archive-hash" }
  }
  expect_failures = [data.aws_lambda_function.verify_upper]
}

run "rejects_reader_secret_not_current" {
  command = plan
  plan_options { target = [data.aws_secretsmanager_secret_version.verify_reader] }

  override_data {
    target = data.aws_secretsmanager_secret_version.verify_reader
    values = { version_stages = ["AWSPREVIOUS"], secret_string = "{\"username\":\"example_reader\",\"password\":\"MockOnlyPassword123456789\"}" }
  }
  expect_failures = [data.aws_secretsmanager_secret_version.verify_reader]
}

run "rejects_wrong_reader_secret_credentials" {
  command = plan
  plan_options { target = [data.aws_secretsmanager_secret_version.verify_reader] }

  override_data {
    target = data.aws_secretsmanager_secret_version.verify_reader
    values = { version_stages = ["AWSCURRENT"], secret_string = "{\"username\":\"unrelated_reader\",\"password\":\"MockOnlyPassword123456789\"}" }
  }

  expect_failures = [data.aws_secretsmanager_secret_version.verify_reader]
}

run "rejects_dynamodb_without_pitr" {
  command = plan
  plan_options { target = [data.aws_dynamodb_table.verify_zero_etl] }

  override_data {
    target = data.aws_dynamodb_table.verify_zero_etl[0]
    values = {
      arn                    = "arn:aws:dynamodb:eu-central-1:111111111111:table/example-zero-etl-source", billing_mode = "PAY_PER_REQUEST", hash_key = "order_id"
      attribute              = [{ name = "order_id", type = "S" }]
      point_in_time_recovery = [{ enabled = false, recovery_period_in_days = 0 }]
    }
  }
  expect_failures = [data.aws_dynamodb_table.verify_zero_etl[0]]
}

run "external_dynamodb_allows_other_key_and_billing" {
  command = apply
  variables {
    zero_etl_create_dynamodb_source = false
    zero_etl_source_arn             = "arn:aws:dynamodb:eu-central-1:111111111111:table/existing-inventory"
  }

  plan_options { target = [data.aws_dynamodb_table.verify_zero_etl] }

  override_data {
    target = data.aws_dynamodb_table.verify_zero_etl[0]
    values = {
      arn                    = "arn:aws:dynamodb:eu-central-1:111111111111:table/existing-inventory", billing_mode = "PROVISIONED", hash_key = "sku"
      attribute              = [{ name = "sku", type = "S" }]
      point_in_time_recovery = [{ enabled = true, recovery_period_in_days = 35 }]
    }
  }

  assert {
    condition     = data.aws_dynamodb_table.verify_zero_etl[0].hash_key == "sku"
    error_message = "Existing DynamoDB sources must not be constrained to the owned fixture's schema or billing mode."
  }
}

run "external_dynamodb_readbacks_plan" {
  command = plan
  variables {
    zero_etl_create_dynamodb_source = false
    zero_etl_source_arn             = "arn:aws:dynamodb:eu-central-1:111111111111:table/existing-inventory"
  }

  plan_options { target = [data.aws_dynamodb_table.verify_zero_etl] }

  override_data {
    target = data.aws_dynamodb_table.verify_zero_etl[0]
    values = {
      arn                    = "arn:aws:dynamodb:eu-central-1:111111111111:table/existing-inventory", billing_mode = "PROVISIONED", hash_key = "sku"
      attribute              = [{ name = "sku", type = "S" }]
      point_in_time_recovery = [{ enabled = true, recovery_period_in_days = 35 }]
    }
  }

  assert {
    condition     = data.aws_dynamodb_table.verify_zero_etl[0].hash_key == "sku" && data.aws_dynamodb_table.verify_zero_etl[0].point_in_time_recovery[0].enabled
    error_message = "The external-table plan must observe PITR without imposing the owned fixture's key or billing mode."
  }
}

run "aurora_source_readbacks" {
  command = apply
  variables {
    zero_etl_create_dynamodb_source = false
    zero_etl_create_source          = true
  }

  plan_options { target = [data.aws_rds_cluster.verify_zero_etl, data.aws_db_instance.verify_zero_etl] }

  assert {
    condition     = length(data.aws_rds_cluster.verify_zero_etl) == 1 && length(data.aws_db_instance.verify_zero_etl) == 1
    error_message = "Owned Aurora sources must execute both cluster and writer lookups."
  }
}

run "aurora_source_readbacks_plan" {
  command = plan
  variables {
    zero_etl_create_dynamodb_source = false
    zero_etl_create_source          = true
  }

  plan_options { target = [data.aws_rds_cluster.verify_zero_etl, data.aws_db_instance.verify_zero_etl] }

  assert {
    condition = (
      data.aws_rds_cluster.verify_zero_etl[0].storage_encrypted && data.aws_db_instance.verify_zero_etl[0].storage_encrypted &&
      data.aws_db_instance.verify_zero_etl[0].db_cluster_identifier == "redshift-oss-e2e-deadbeef-zero-etl-source" &&
      !data.aws_db_instance.verify_zero_etl[0].publicly_accessible
    )
    error_message = "The Aurora plan must observe an encrypted source cluster and its private serverless writer."
  }
}

run "rejects_public_aurora_writer" {
  command = plan
  variables {
    zero_etl_create_dynamodb_source = false
    zero_etl_create_source          = true
  }

  plan_options { target = [data.aws_db_instance.verify_zero_etl] }

  override_data {
    target = data.aws_db_instance.verify_zero_etl[0]
    values = {
      db_cluster_identifier = "redshift-oss-e2e-deadbeef-zero-etl-source", engine = "aurora-postgresql", db_instance_class = "db.serverless"
      storage_encrypted     = true, publicly_accessible = true
    }
  }

  expect_failures = [data.aws_db_instance.verify_zero_etl[0]]
}

run "identity_center_service_readbacks" {
  command = apply
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }

  plan_options {
    target = [data.aws_ssoadmin_application.verify_consumer, data.aws_ssoadmin_application_assignments.verify_consumer, data.aws_identitystore_group.verify_consumer, data.aws_vpc_endpoint.verify_consumer, data.aws_iam_role_policies.verify_consumer]
  }

  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["sso_oauth"]
    values = { service_name = "com.amazonaws.eu-central-1.sso-oauth" }
  }
  override_data {
    target = module.consumer_endpoints.data.aws_vpc_endpoint_service.this["identitystore"]
    values = { service_name = "com.amazonaws.eu-central-1.identitystore" }
  }

  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }

  assert {
    condition = (
      length(data.aws_ssoadmin_application.verify_consumer) == 1 &&
      length(data.aws_ssoadmin_application_assignments.verify_consumer) == 1 &&
      length(data.aws_identitystore_group.verify_consumer) == 2 && length(data.aws_vpc_endpoint.verify_consumer) == 4
    )
    error_message = "SSO must execute managed-application, exact-assignment, directory-group, and private-endpoint readbacks."
  }
}

run "identity_center_readbacks_plan" {
  command = plan
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }

  plan_options {
    target = [data.aws_ssoadmin_application.verify_consumer, data.aws_ssoadmin_application_assignments.verify_consumer, data.aws_identitystore_group.verify_consumer, data.aws_vpc_endpoint.verify_consumer, data.aws_iam_role_policies.verify_consumer]
  }

  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }

  assert {
    condition = (
      data.aws_ssoadmin_application.verify_consumer[0].status == "ENABLED" &&
      length(data.aws_ssoadmin_application_assignments.verify_consumer[0].application_assignments) == 2 &&
      data.aws_identitystore_group.verify_consumer["readers"].display_name == "redshift-oss-e2e-deadbeef-readers" &&
      data.aws_identitystore_group.verify_consumer["operators"].display_name == "redshift-oss-e2e-deadbeef-operators" &&
      data.aws_vpc_endpoint.verify_consumer["sso_oauth"].private_dns_enabled &&
      data.aws_vpc_endpoint.verify_consumer["identitystore"].private_dns_enabled &&
      contains(data.aws_iam_role_policies.verify_consumer.policy_names, "identity-center")
    )
    error_message = "The SSO plan must evaluate the application, assignments, directory names, IAM attachment inventory, and private endpoints."
  }
}

run "rejects_extra_sso_assignment" {
  command = plan
  variables {
    identity_center_instance_arn = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
  }

  plan_options { target = [data.aws_ssoadmin_application_assignments.verify_consumer] }

  override_data {
    target = data.aws_ssoadmin_application_assignments.verify_consumer[0]
    values = {
      application_assignments = [
        { principal_type = "GROUP", principal_id = "11111111-1111-1111-1111-111111111111", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
        { principal_type = "GROUP", principal_id = "22222222-2222-2222-2222-222222222222", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
        { principal_type = "USER", principal_id = "unexpected-user", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
      ]
    }
  }

  expect_failures = [data.aws_ssoadmin_application_assignments.verify_consumer[0]]
}

run "explicit_public_sql_ingress_readbacks" {
  command = apply
  variables {
    allow_public_sql = true
    public_sql_cidrs = ["203.0.113.10/32"]
  }

  plan_options { target = [data.aws_vpc_security_group_rule.verify_producer_sql, data.aws_vpc_security_group_rule.verify_consumer_sql] }

  assert {
    condition     = length(data.aws_vpc_security_group_rule.verify_producer_sql) == 1 && length(data.aws_vpc_security_group_rule.verify_consumer_sql) == 1
    error_message = "Public SQL mode must independently read each explicitly authorized client ingress rule in both accounts."
  }
}

run "rejects_public_sql_port_range" {
  command = plan
  variables {
    allow_public_sql = true
    public_sql_cidrs = ["203.0.113.10/32"]
  }

  plan_options { target = [data.aws_vpc_security_group_rule.verify_consumer_sql] }

  override_data {
    target = data.aws_vpc_security_group_rule.verify_consumer_sql["203.0.113.10/32"]
    values = { is_egress = false, security_group_id = "sg-consumer", ip_protocol = "tcp", from_port = 0, to_port = 65535, cidr_ipv4 = "203.0.113.10/32" }
  }

  expect_failures = [data.aws_vpc_security_group_rule.verify_consumer_sql["203.0.113.10/32"]]
}

# These targeted seeds never select SQL readback data sources. Native AWS mocks populate the matching topology;
# the following plans then evaluate independent observations rather than accepting unknown apply-time results.
run "public_service_fixture" {
  command = apply
  variables {
    allow_public_sql = true
    public_sql_cidrs = ["203.0.113.10/32"]
  }

  plan_options {
    target = [aws_redshift_cluster.producer, aws_redshiftserverless_workgroup.consumer, aws_vpc_security_group_ingress_rule.producer_sql, aws_vpc_security_group_ingress_rule.consumer_sql]
  }
}

run "public_service_readbacks_plan" {
  command = plan
  variables {
    allow_public_sql = true
    public_sql_cidrs = ["203.0.113.10/32"]
  }

  plan_options {
    target = [
      data.aws_vpc.verify_producer, data.aws_vpc.verify_consumer,
      data.aws_subnet.verify_producer, data.aws_subnet.verify_consumer,
      data.aws_route_table.verify_producer, data.aws_route_table.verify_consumer,
      data.aws_vpc_endpoint.verify_producer, data.aws_vpc_endpoint.verify_consumer,
      data.aws_vpc_security_group_rules.verify_producer, data.aws_vpc_security_group_rules.verify_consumer,
      data.aws_vpc_security_group_rule.verify_producer_sql, data.aws_vpc_security_group_rule.verify_consumer_sql,
      data.aws_vpc_security_group_rule.verify_producer_egress, data.aws_vpc_security_group_rule.verify_consumer_egress,
      data.aws_redshift_cluster.verify_producer, data.aws_redshiftserverless_workgroup.verify_consumer,
    ]
  }

  # Copy these eight observed-response overrides into any full public composition run using this client CIDR.
  override_data {
    target = data.aws_route_table.verify_producer
    values = {
      vpc_id = "vpc-producer"
      routes = [
        { cidr_block = "10.42.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
        { cidr_block = "", gateway_id = "vpce-producer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-producer-s3" },
        { cidr_block = "0.0.0.0/0", gateway_id = "igw-producer", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      ]
    }
  }
  override_data {
    target = data.aws_route_table.verify_consumer
    values = {
      vpc_id = "vpc-consumer"
      routes = [
        { cidr_block = "10.43.0.0/16", gateway_id = "local", ipv6_cidr_block = "", destination_prefix_list_id = "" },
        { cidr_block = "", gateway_id = "vpce-consumer-s3", ipv6_cidr_block = "", destination_prefix_list_id = "pl-consumer-s3" },
        { cidr_block = "0.0.0.0/0", gateway_id = "igw-consumer", ipv6_cidr_block = "", destination_prefix_list_id = "" },
      ]
    }
  }
  override_data {
    target = data.aws_vpc_endpoint.verify_producer["s3"]
    values = {
      state           = "available", vpc_id = "vpc-producer", service_name = "com.amazonaws.eu-central-1.s3", vpc_endpoint_type = "Gateway"
      route_table_ids = ["rtb-producer-public"]
    }
  }
  override_data {
    target = data.aws_vpc_endpoint.verify_consumer["s3"]
    values = {
      state           = "available", vpc_id = "vpc-consumer", service_name = "com.amazonaws.eu-central-1.s3", vpc_endpoint_type = "Gateway"
      route_table_ids = ["rtb-consumer-public"]
    }
  }
  override_data {
    target = data.aws_vpc_security_group_rules.verify_producer
    values = { ids = ["sgr-producer-vpc", "sgr-producer-s3", "sgr-producer-client"] }
  }
  override_data {
    target = data.aws_vpc_security_group_rules.verify_consumer
    values = { ids = ["sgr-consumer-vpc", "sgr-consumer-s3", "sgr-consumer-client"] }
  }
  override_data {
    target = data.aws_redshift_cluster.verify_producer
    values = {
      encrypted                    = true, enhanced_vpc_routing = true, publicly_accessible = true, vpc_id = "vpc-producer"
      node_type                    = "ra3.large", database_name = "dev", master_username = "tfadmin", port = 5439
      cluster_subnet_group_name    = "redshift-oss-e2e-deadbeef-producer"
      cluster_parameter_group_name = "redshift-oss-e2e-deadbeef-producer"
      vpc_security_group_ids       = ["sg-producer"]
      iam_roles                    = ["arn:aws:iam::111111111111:role/producer-fixture"]
      default_iam_role_arn         = "arn:aws:iam::111111111111:role/producer-fixture"
      cluster_namespace_arn        = "arn:aws:redshift:eu-central-1:111111111111:namespace:11111111-2222-3333-4444-555555555555"
    }
  }
  override_data {
    target = data.aws_redshiftserverless_workgroup.verify_consumer
    values = {
      namespace_name     = "redshift-oss-e2e-deadbeef-consumer", enhanced_vpc_routing = true, publicly_accessible = true
      subnet_ids         = ["subnet-consumer-0", "subnet-consumer-1", "subnet-consumer-2"]
      security_group_ids = ["sg-consumer"]
      endpoint           = [{ address = "consumer.example.test", port = 5439 }]
    }
  }

  assert {
    condition = (
      data.aws_redshift_cluster.verify_producer.publicly_accessible && data.aws_redshiftserverless_workgroup.verify_consumer.publicly_accessible &&
      alltrue([for route_table in data.aws_route_table.verify_producer : anytrue([for route in route_table.routes : route.cidr_block == "0.0.0.0/0" && route.gateway_id == "igw-producer"])]) &&
      alltrue([for route_table in data.aws_route_table.verify_consumer : anytrue([for route in route_table.routes : route.cidr_block == "0.0.0.0/0" && route.gateway_id == "igw-consumer"])]) &&
      data.aws_vpc_endpoint.verify_producer["s3"].route_table_ids == toset(["rtb-producer-public"]) &&
      data.aws_vpc_endpoint.verify_consumer["s3"].route_table_ids == toset(["rtb-consumer-public"]) &&
      data.aws_vpc_security_group_rule.verify_producer_sql["203.0.113.10/32"].from_port == 5439 &&
      data.aws_vpc_security_group_rule.verify_consumer_sql["203.0.113.10/32"].to_port == 5439
    )
    error_message = "The public plan must observe public warehouse endpoints, real internet/S3 routes, and only the explicitly allowed SQL client."
  }
}

run "cross_account_service_fixture" {
  command   = apply
  state_key = "aws-cross-account"

  plan_options {
    target = [aws_redshift_cluster.producer, aws_redshiftserverless_workgroup.consumer, aws_lambda_function.extfunctions_upper, aws_iam_role_policy.consumer_lambda_udf, aws_iam_role_policy.producer_spectrum_tables, aws_glue_catalog_table.fixture, aws_redshift_integration.zero_etl, module.reader_secret]
  }

  # These account/managed-output overrides are also required by the cross-account readback plan below.
  override_data {
    target = data.aws_caller_identity.consumer
    values = { account_id = "222222222222" }
  }
  override_resource {
    target = aws_iam_role.consumer
    values = { id = "redshift-oss-e2e-deadbeef-consumer", arn = "arn:aws:iam::222222222222:role/consumer-fixture" }
  }
  override_resource {
    target = aws_iam_role.extfunctions_lambda
    values = { id = "redshift-oss-e2e-deadbeef-udf-exec", arn = "arn:aws:iam::222222222222:role/lambda-udf-exec" }
  }
  override_resource {
    target = aws_redshiftserverless_namespace.consumer
    values = {
      namespace_id              = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      arn                       = "arn:aws:redshift-serverless:eu-central-1:222222222222:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      admin_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:consumer-admin-ABC123"
    }
  }
  override_resource {
    target = aws_dynamodb_table.zero_etl_source[0]
    values = { arn = "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source" }
  }
  override_resource {
    target = aws_redshiftserverless_workgroup.consumer
    values = {
      arn      = "arn:aws:redshift-serverless:eu-central-1:222222222222:workgroup/bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
      port     = 5439
      endpoint = [{ address = "consumer.example.test", port = 5439 }]
    }
  }
  override_resource {
    target = aws_lambda_function.extfunctions_upper
    values = { arn = "arn:aws:lambda:eu-central-1:222222222222:function:redshift-oss-e2e-deadbeef-upper" }
  }
  override_resource {
    target = aws_redshift_integration.zero_etl[0]
    values = { arn = "arn:aws:redshift:eu-central-1:222222222222:integration:a1b2c3d4-5678-90ab-cdef-example11111" }
  }
  override_resource {
    target = module.reader_secret.aws_secretsmanager_secret.this[0]
    values = { arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:reader-ABC123" }
  }
}

run "cross_account_service_readbacks_plan" {
  command   = plan
  state_key = "aws-cross-account"

  plan_options {
    target = [
      data.aws_vpc.verify_producer, data.aws_vpc.verify_consumer,
      data.aws_subnet.verify_producer, data.aws_subnet.verify_consumer,
      data.aws_route_table.verify_producer, data.aws_route_table.verify_consumer,
      data.aws_vpc_endpoint.verify_producer, data.aws_vpc_endpoint.verify_consumer,
      data.aws_vpc_security_group_rules.verify_producer, data.aws_vpc_security_group_rules.verify_consumer,
      data.aws_vpc_security_group_rule.verify_producer_egress, data.aws_vpc_security_group_rule.verify_consumer_egress,
      data.aws_redshift_cluster.verify_producer, data.aws_redshift_subnet_group.verify_producer,
      data.aws_redshiftserverless_namespace.verify_consumer, data.aws_redshiftserverless_workgroup.verify_consumer,
      data.aws_iam_role.verify_producer, data.aws_iam_role.verify_consumer, data.aws_iam_role.verify_lambda,
      data.aws_iam_role_policies.verify_producer, data.aws_iam_role_policies.verify_consumer, data.aws_iam_role_policy_attachments.verify_lambda,
      data.aws_s3_bucket.verify_fixture, data.aws_s3_bucket_policy.verify_fixture, data.aws_s3_object.verify_fixture,
      data.aws_glue_catalog_table.verify_fixture, data.aws_lambda_function.verify_upper,
      data.aws_secretsmanager_secret_version.verify_reader, data.aws_dynamodb_table.verify_zero_etl,
    ]
  }

  # Copy all overrides in this run into a full cross-account composition run, using an isolated state_key.
  override_data {
    target = data.aws_caller_identity.consumer
    values = { account_id = "222222222222" }
  }
  override_resource {
    target = aws_iam_role.consumer
    values = { id = "redshift-oss-e2e-deadbeef-consumer", arn = "arn:aws:iam::222222222222:role/consumer-fixture" }
  }
  override_resource {
    target = aws_iam_role.extfunctions_lambda
    values = { id = "redshift-oss-e2e-deadbeef-udf-exec", arn = "arn:aws:iam::222222222222:role/lambda-udf-exec" }
  }
  override_resource {
    target = aws_redshiftserverless_namespace.consumer
    values = {
      namespace_id              = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      arn                       = "arn:aws:redshift-serverless:eu-central-1:222222222222:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      admin_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:consumer-admin-ABC123"
    }
  }
  override_resource {
    target = aws_dynamodb_table.zero_etl_source[0]
    values = { arn = "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source" }
  }
  override_resource {
    target = aws_redshiftserverless_workgroup.consumer
    values = {
      arn      = "arn:aws:redshift-serverless:eu-central-1:222222222222:workgroup/bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
      port     = 5439
      endpoint = [{ address = "consumer.example.test", port = 5439 }]
    }
  }
  override_resource {
    target = aws_lambda_function.extfunctions_upper
    values = { arn = "arn:aws:lambda:eu-central-1:222222222222:function:redshift-oss-e2e-deadbeef-upper" }
  }
  override_resource {
    target = aws_redshift_integration.zero_etl[0]
    values = { arn = "arn:aws:redshift:eu-central-1:222222222222:integration:a1b2c3d4-5678-90ab-cdef-example11111" }
  }
  override_resource {
    target = module.reader_secret.aws_secretsmanager_secret.this[0]
    values = { arn = "arn:aws:secretsmanager:eu-central-1:222222222222:secret:reader-ABC123" }
  }
  override_data {
    target = data.aws_vpc.verify_consumer
    values = { cidr_block = "10.43.0.0/16", owner_id = "222222222222", enable_dns_support = true, enable_dns_hostnames = true }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["0"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.0.0/24", availability_zone = "eu-central-1a", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["1"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.1.0/24", availability_zone = "eu-central-1b", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_subnet.verify_consumer["2"]
    values = { vpc_id = "vpc-consumer", cidr_block = "10.43.2.0/24", availability_zone = "eu-central-1c", owner_id = "222222222222", map_public_ip_on_launch = false }
  }
  override_data {
    target = data.aws_redshiftserverless_namespace.verify_consumer
    values = {
      arn                  = "arn:aws:redshift-serverless:eu-central-1:222222222222:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      namespace_id         = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
      db_name              = "dev", admin_username = "tfadmin"
      default_iam_role_arn = "arn:aws:iam::222222222222:role/consumer-fixture"
      iam_roles            = ["arn:aws:iam::222222222222:role/consumer-fixture"]
    }
  }
  override_data {
    target = data.aws_iam_role.verify_consumer
    values = {
      arn = "arn:aws:iam::222222222222:role/consumer-fixture"
      assume_role_policy = jsonencode({
        Version = "2012-10-17"
        Statement = [{
          Effect    = "Allow", Principal = { Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"] }
          Action    = ["sts:AssumeRole", "sts:SetContext"]
          Condition = { StringEquals = { "aws:SourceAccount" = "222222222222" } }
        }]
      })
    }
  }
  override_data {
    target = data.aws_iam_role.verify_lambda
    values = {
      arn = "arn:aws:iam::222222222222:role/lambda-udf-exec"
      assume_role_policy = jsonencode({
        Version   = "2012-10-17"
        Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
      })
    }
  }
  override_data {
    target = data.aws_lambda_function.verify_upper
    values = { role = "arn:aws:iam::222222222222:role/lambda-udf-exec", runtime = "python3.12", handler = "index.handler", code_sha256 = "uQKxXeOixWb6cYdvULKBSzw2FBs+2DGpSF89PjXrSfY=" }
  }
  override_data {
    target = data.aws_dynamodb_table.verify_zero_etl[0]
    values = {
      arn                    = "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source", billing_mode = "PAY_PER_REQUEST", hash_key = "order_id"
      attribute              = [{ name = "order_id", type = "S" }]
      point_in_time_recovery = [{ enabled = true, recovery_period_in_days = 35 }]
    }
  }
  override_data {
    target = data.aws_s3_bucket_policy.verify_fixture
    values = {
      policy = jsonencode({
        Version = "2012-10-17"
        Statement = [
          {
            Sid       = "RequireTLS", Effect = "Deny", Principal = "*", Action = "s3:*"
            Resource  = ["arn:aws:s3:::mock-fixture", "arn:aws:s3:::mock-fixture/*"]
            Condition = { Bool = { "aws:SecureTransport" = "false" } }
          },
          {
            Sid      = "RedshiftEventIntegration", Effect = "Allow", Principal = { Service = "redshift.amazonaws.com" }
            Action   = ["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]
            Resource = ["arn:aws:s3:::mock-fixture"]
            Condition = {
              StringEquals = { "aws:SourceAccount" = "111111111111" }
              ArnLike      = { "aws:SourceArn" = "arn:aws:redshift:eu-central-1:111111111111:integration:*" }
            }
          },
          {
            Sid    = "ConsumerBucketMetadata", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:GetBucketLocation", "s3:ListBucket"], Resource = ["arn:aws:s3:::mock-fixture"]
          },
          {
            Sid    = "ConsumerFixtureRead", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:GetObject"], Resource = ["arn:aws:s3:::mock-fixture/data/*", "arn:aws:s3:::mock-fixture/unload/*"]
          },
          {
            Sid    = "ConsumerUnload", Effect = "Allow", Principal = { AWS = "arn:aws:iam::222222222222:role/consumer-fixture" }
            Action = ["s3:PutObject", "s3:AbortMultipartUpload"], Resource = ["arn:aws:s3:::mock-fixture/unload/*"]
          },
        ]
      })
    }
  }

  assert {
    condition = (
      data.aws_vpc.verify_consumer.owner_id == "222222222222" &&
      alltrue([for subnet in data.aws_subnet.verify_consumer : subnet.owner_id == "222222222222"]) &&
      data.aws_iam_role.verify_consumer.arn == "arn:aws:iam::222222222222:role/consumer-fixture" &&
      data.aws_iam_role.verify_lambda.arn == "arn:aws:iam::222222222222:role/lambda-udf-exec" &&
      data.aws_redshiftserverless_namespace.verify_consumer.default_iam_role_arn == "arn:aws:iam::222222222222:role/consumer-fixture" &&
      data.aws_lambda_function.verify_upper.role == "arn:aws:iam::222222222222:role/lambda-udf-exec" &&
      data.aws_dynamodb_table.verify_zero_etl[0].arn == "arn:aws:dynamodb:eu-central-1:222222222222:table/example-zero-etl-source" &&
      length(jsondecode(data.aws_s3_bucket_policy.verify_fixture.policy).Statement) == 5
    )
    error_message = "The cross-account plan must observe the consumer's real ownership/role bindings and the producer bucket's exact cross-account fixture grants."
  }
}

run "existing_identity_center_groups_fixture" {
  command   = apply
  state_key = "aws-existing-directory-groups"
  variables {
    identity_center_instance_arn        = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    identity_center_reader_group_name   = "existing-developers"
    identity_center_operator_group_name = "existing-devops"
  }

  plan_options {
    target = [aws_ssoadmin_application_assignment.readers, aws_ssoadmin_application_assignment.operators, module.consumer_endpoints, aws_iam_role_policy.consumer_fixture, aws_iam_role_policy.consumer_lambda_udf]
  }

  override_data {
    target = data.aws_identitystore_group.readers[0]
    values = { group_id = "33333333-3333-3333-3333-333333333333", display_name = "existing-developers" }
  }
  override_data {
    target = data.aws_identitystore_group.operators[0]
    values = { group_id = "44444444-4444-4444-4444-444444444444", display_name = "existing-devops" }
  }
}

run "existing_identity_center_groups_readbacks_plan" {
  command   = plan
  state_key = "aws-existing-directory-groups"
  variables {
    identity_center_instance_arn        = "arn:aws:sso:::instance/ssoins-1234567890abcdef"
    identity_center_reader_group_name   = "existing-developers"
    identity_center_operator_group_name = "existing-devops"
  }

  plan_options {
    target = [data.aws_ssoadmin_application.verify_consumer, data.aws_ssoadmin_application_assignments.verify_consumer, data.aws_identitystore_group.verify_consumer, data.aws_vpc_endpoint.verify_consumer, data.aws_iam_role_policies.verify_consumer]
  }

  # Copy all six overrides into an existing-group composition run; directory IDs must be valid UUIDs.
  override_data {
    target = data.aws_identitystore_group.readers[0]
    values = { group_id = "33333333-3333-3333-3333-333333333333", display_name = "existing-developers" }
  }
  override_data {
    target = data.aws_identitystore_group.operators[0]
    values = { group_id = "44444444-4444-4444-4444-444444444444", display_name = "existing-devops" }
  }
  override_data {
    target = data.aws_identitystore_group.verify_consumer["readers"]
    values = { display_name = "existing-developers" }
  }
  override_data {
    target = data.aws_identitystore_group.verify_consumer["operators"]
    values = { display_name = "existing-devops" }
  }
  override_data {
    target = data.aws_ssoadmin_application_assignments.verify_consumer[0]
    values = {
      application_assignments = [
        { principal_type = "GROUP", principal_id = "33333333-3333-3333-3333-333333333333", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
        { principal_type = "GROUP", principal_id = "44444444-4444-4444-4444-444444444444", application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" },
      ]
    }
  }
  override_data {
    target = data.aws_iam_role_policies.verify_consumer
    values = { policy_names = ["fixture", "lambda-udf", "identity-center"] }
  }

  assert {
    condition = (
      length(aws_identitystore_group.readers) == 0 && length(aws_identitystore_group.operators) == 0 &&
      data.aws_identitystore_group.verify_consumer["readers"].display_name == "existing-developers" &&
      data.aws_identitystore_group.verify_consumer["operators"].display_name == "existing-devops" &&
      toset([for assignment in data.aws_ssoadmin_application_assignments.verify_consumer[0].application_assignments : assignment.principal_id]) == toset(["33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444"])
    )
    error_message = "Existing directory groups must be observed and assigned without creating replacement groups or directory memberships."
  }
}
