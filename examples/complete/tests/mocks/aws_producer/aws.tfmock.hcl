# Policy-document merging is an AWS-provider data source inside the bucket module.
# composition.tftest.hcl asserts the configured policy; the mock must still return valid JSON.
mock_data "aws_iam_policy_document" {
  defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
}
mock_data "aws_availability_zones" {
  defaults = { names = ["eu-central-1a", "eu-central-1b", "eu-central-1c"] }
}
mock_data "aws_partition" {
  defaults = { partition = "aws" }
}
mock_data "aws_caller_identity" {
  defaults = { account_id = "111111111111" }
}
mock_resource "aws_redshift_cluster" {
  defaults = {
    cluster_type               = "single-node"
    cluster_namespace_arn      = "arn:aws:redshift:eu-central-1:111111111111:namespace:11111111-2222-3333-4444-555555555555"
    master_password_secret_arn = "arn:aws:secretsmanager:eu-central-1:111111111111:secret:producer-admin-ABC123"
    dns_name                   = "producer.example.test"
  }
}
mock_resource "aws_iam_role" {
  defaults = { arn = "arn:aws:iam::111111111111:role/producer-fixture" }
}
mock_resource "aws_s3_bucket" {
  defaults = { arn = "arn:aws:s3:::mock-fixture" }
}
mock_resource "aws_s3_object" {
  # Uploads inherit the fixture bucket's SSE-S3 encryption.
  defaults = { server_side_encryption = "AES256" }
}
mock_resource "aws_glue_catalog_database" {
  defaults = { arn = "arn:aws:glue:eu-central-1:111111111111:database/mock_fixture" }
}
