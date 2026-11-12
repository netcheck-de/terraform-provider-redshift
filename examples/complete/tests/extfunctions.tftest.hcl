# Mocked runs for external functions and routine lookups. The mocks are shared with composition.tftest.hcl through tests/mocks, so
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
mock_provider "redshift" { alias = "producer_data_api_iam" }
mock_provider "redshift" { alias = "consumer_data_api_iam" }
mock_provider "redshift" { alias = "producer_direct_iam" }
mock_provider "redshift" { alias = "consumer_direct_iam" }
mock_provider "redshift" { alias = "producer_direct_password" }
mock_provider "redshift" { alias = "consumer_direct_password" }

override_resource {
  target = module.fixture_bucket.aws_s3_bucket.this[0]
  values = { id = "mock-fixture", arn = "arn:aws:s3:::mock-fixture" }
}

run "extfunction_missing_overload_blocks_apply" {
  command = apply
  plan_options { target = [data.redshift_external_functions.example] }
  # Terraform's generated empty nested list is a missing catalog overload, not a valid readback.
  expect_failures = [data.redshift_external_functions.example]
}

run "extfunction_missing_parameters_block_apply" {
  command = apply
  plan_options { target = [data.redshift_routine_parameters.upper] }
  expect_failures = [data.redshift_routine_parameters.upper]
}

run "extfunctions_apply" {
  command = apply

  assert {
    condition = (
      redshift_external_function.upper.database == redshift_schema.local.database &&
      redshift_external_function.upper.schema == redshift_schema.local.name &&
      redshift_external_function.upper.arguments == tolist(["VARCHAR"]) &&
      redshift_external_function.upper.lambda_function == aws_lambda_function.extfunctions_upper.function_name &&
      redshift_external_function.upper.iam_role == aws_iam_role.consumer.arn
    )
    error_message = "The Lambda UDF must live in the consumer schema and invoke the example Lambda function through the consumer role."
  }

  assert {
    condition = (
      jsondecode(aws_iam_role_policy.consumer_lambda_udf.policy).Statement[0].Resource == aws_lambda_function.extfunctions_upper.arn &&
      aws_iam_role_policy.consumer_lambda_udf.role == aws_iam_role.consumer.id &&
      aws_lambda_function.extfunctions_upper.role == aws_iam_role.extfunctions_lambda.arn &&
      aws_lambda_function.extfunctions_upper.source_code_hash == data.archive_file.extfunctions_upper.output_base64sha256
    )
    error_message = "Only the consumer warehouse role may invoke the packaged Lambda function."
  }

  assert {
    condition = (
      data.redshift_external_functions.example.database == redshift_external_function.upper.database &&
      data.redshift_external_functions.example.schema == redshift_external_function.upper.schema &&
      data.redshift_external_functions.example.name == redshift_external_function.upper.name &&
      data.redshift_routine_parameters.upper.routine_name == redshift_external_function.upper.name &&
      data.redshift_routine_parameters.upper.routine_type == "FUNCTION"
    )
    error_message = "The routine listings must be filtered to the managed function and its schema."
  }

  # Terraform 1.17 cannot populate computed nested lists. These outputs remain empty under mocks;
  # the real catalog postconditions still reject that incomplete readback instead of accepting configuration.
  assert {
    condition = (
      output.extfunction_checks.lambda_udfs == [] &&
      output.extfunction_checks.lambda_owners == {} &&
      output.extfunction_checks.upper_parameters == []
    )
    error_message = "The routine checks must expose only what the routine listings observe."
  }
}
