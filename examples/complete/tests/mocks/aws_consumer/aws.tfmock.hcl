mock_data "aws_availability_zones" {
  defaults = { names = ["eu-central-1a", "eu-central-1b", "eu-central-1c"] }
}
mock_data "aws_partition" {
  defaults = { partition = "aws" }
}
mock_data "aws_caller_identity" {
  defaults = { account_id = "111111111111" }
}
mock_resource "aws_redshiftserverless_namespace" {
  defaults = {
    namespace_id              = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    arn                       = "arn:aws:redshift-serverless:eu-central-1:111111111111:namespace/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
    admin_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:111111111111:secret:consumer-admin-ABC123"
  }
}
mock_resource "aws_redshiftserverless_workgroup" {
  defaults = {
    port     = 5439
    endpoint = [{ address = "consumer.example.test", port = 5439 }]
  }
}
mock_resource "aws_iam_role" {
  defaults = { arn = "arn:aws:iam::111111111111:role/consumer-fixture" }
}
mock_resource "aws_secretsmanager_secret" {
  defaults = { arn = "arn:aws:secretsmanager:eu-central-1:111111111111:secret:reader-ABC123" }
}
mock_data "aws_ssoadmin_instances" {
  defaults = {
    arns               = ["arn:aws:sso:::instance/ssoins-1234567890abcdef"]
    identity_store_ids = ["d-1234567890"]
  }
}
mock_resource "aws_redshift_idc_application" {
  defaults = { idc_managed_application_arn = "arn:aws:sso::111111111111:application/ssoins-1234567890abcdef/apl-1234567890abcdef" }
}
