mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "123456789012"
      arn        = "arn:aws:sts::123456789012:assumed-role/redshift-ci/test"
    }
  }
  mock_data "aws_availability_zones" {
    defaults = { names = ["eu-central-1a", "eu-central-1b", "eu-central-1c"] }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_redshift_cluster" {
    defaults = {
      cluster_namespace_arn      = "arn:aws:redshift:eu-central-1:123456789012:namespace:11111111-2222-3333-4444-555555555555"
      master_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:123456789012:secret:producer-admin-abcdef"
    }
  }
  mock_resource "aws_s3_bucket" {
    defaults = { arn = "arn:aws:s3:::redshift-ci-validation" }
  }
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/acceptance" }
  }
  mock_resource "aws_redshiftserverless_namespace" {
    defaults = {
      arn                       = "arn:aws:redshift-serverless:eu-central-1:123456789012:namespace/11111111-2222-3333-4444-555555555555"
      namespace_id              = "11111111-2222-3333-4444-555555555555"
      admin_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:123456789012:secret:consumer-admin-abcdef"
    }
  }
}
mock_provider "archive" {}
mock_provider "redshift" {}
mock_provider "redshift" { alias = "producer" }

variables {
  region      = "eu-central-1"
  name_prefix = "redshift-ci-validation"
  runner_cidr = "8.8.8.8/32"
  db_name     = "acceptance_fixture"
}

run "acceptance_fixture" {
  command = apply

  assert {
    condition = (
      aws_redshiftserverless_workgroup.this.base_capacity == 4 &&
      aws_redshiftserverless_workgroup.this.max_capacity == 4 &&
      length(module.vpc.redshift_subnets) == 3 &&
      aws_vpc_security_group_ingress_rule.this.cidr_ipv4 == var.runner_cidr &&
      output.acceptance.REDSHIFT_ACC_WORKGROUP == aws_redshiftserverless_workgroup.this.workgroup_name &&
      output.acceptance.REDSHIFT_ACC_REGION == var.region &&
      output.acceptance.REDSHIFT_ACC_DATABASE == aws_redshiftserverless_namespace.this.db_name &&
      output.acceptance.REDSHIFT_ACC_DATABASE == var.db_name &&
      output.acceptance.REDSHIFT_ACC_SECRET_ARN == aws_redshiftserverless_namespace.this.admin_password_secret_arn &&
      output.acceptance.REDSHIFT_ACC_PARQUET_LOCATION == local.parquet_location &&
      output.acceptance.REDSHIFT_ACC_ASSUMEROLE == "1"
    )
    error_message = "Acceptance must retain bounded Serverless compute, runner ingress and standard fixture inputs."
  }
  assert {
    condition = (
      aws_redshift_cluster.producer.cluster_subnet_group_name == module.vpc.redshift_subnet_group &&
      contains(aws_redshiftserverless_namespace.this.iam_roles, aws_iam_role.this.arn) &&
      contains(aws_redshiftserverless_namespace.this.iam_roles, aws_iam_role.lambda_invocation.arn) &&
      aws_redshift_integration.copy.target_arn == aws_redshiftserverless_namespace.this.arn &&
      jsondecode(aws_redshift_resource_policy.inbound_integrations.policy).Statement[0].Condition.StringEquals["aws:SourceArn"] == module.s3.s3_bucket_arn &&
      aws_lambda_function.extfunctions_upper.role == aws_iam_role.lambda_execution.arn
    )
    error_message = "Every acceptance run must wire both warehouses, Lambda and the COPY inbound policy."
  }
  assert {
    condition = (
      aws_redshiftdata_statement.source_seed.database == redshift_table.source.database &&
      strcontains(aws_redshiftdata_statement.source_seed.sql, format("\"%s\".\"%s\"", redshift_table.source.schema, redshift_table.source.name))
    )
    error_message = "Seeding must use the managed source table's database and qualified name."
  }
  assert {
    condition = (
      output.acceptance.REDSHIFT_ACC_COPY_SOURCE == local.copy_source &&
      output.acceptance.REDSHIFT_ACC_WORKGROUP == aws_redshiftserverless_workgroup.this.workgroup_name &&
      output.acceptance.REDSHIFT_ACC_REGION == var.region &&
      output.acceptance.REDSHIFT_ACC_DATABASE == aws_redshiftserverless_namespace.this.db_name &&
      output.acceptance.REDSHIFT_ACC_DATABASE == var.db_name &&
      output.acceptance.REDSHIFT_ACC_CLUSTER == aws_redshift_cluster.producer.cluster_identifier &&
      output.acceptance.REDSHIFT_ACC_SECRET_ARN == aws_redshiftserverless_namespace.this.admin_password_secret_arn &&
      output.acceptance.REDSHIFT_ACC_CLUSTER_SECRET_ARN == aws_redshift_cluster.producer.master_password_secret_arn &&
      output.acceptance.REDSHIFT_ACC_SYSTEM_SETTINGS == "1" &&
      output.acceptance.REDSHIFT_ACC_USER_LOCKOUTS == "1" &&
      output.acceptance.REDSHIFT_ACC_SHARED_DATABASE == redshift_database.shared.name &&
      output.acceptance.REDSHIFT_ACC_SHARED_SCHEMA == redshift_schema.source.name &&
      output.acceptance.REDSHIFT_ACC_SHARED_TABLE == redshift_table.source.name &&
      output.acceptance.REDSHIFT_ACC_COPY_JOB_NAME == redshift_copy_job.this.name &&
      !contains(keys(output.acceptance), "REDSHIFT_ACC_DYNAMODB_INTEGRATION_ARN")
    )
    error_message = "Acceptance inputs must enable all configured tests and select the correct warehouse secrets."
  }
}

run "bootstrap_statements" {
  command = apply

  assert {
    condition = (
      aws_redshiftdata_statement.iam_identity.secret_arn == null &&
      aws_redshiftdata_statement.iam_identity.sql == "SELECT current_user" &&
      aws_redshiftdata_statement.iam_superuser.sql == "ALTER USER \"IAMR:redshift-ci\" CREATEUSER" &&
      aws_redshiftdata_statement.iam_superuser.secret_arn == aws_redshiftserverless_namespace.this.admin_password_secret_arn &&
      aws_redshiftdata_statement.public_assumerole.sql == "REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR ALL"
    )
    error_message = "Terraform must create the IAM SQL identity before elevation and revoke PUBLIC's engine-wide default."
  }
}

run "bootstrap_rejects_non_role_credentials" {
  command = plan

  override_data {
    target = data.aws_caller_identity.fixture
    values = {
      account_id = "123456789012"
      arn        = "arn:aws:iam::123456789012:user/not-a-ci-role"
    }
  }

  expect_failures = [aws_redshiftdata_statement.iam_identity]
}
