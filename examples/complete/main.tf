data "aws_caller_identity" "producer" { provider = aws.producer }
data "aws_caller_identity" "consumer" { provider = aws.consumer }
data "aws_partition" "consumer" { provider = aws.consumer }

resource "random_id" "environment" { byte_length = 4 }

locals {
  name             = "${var.name_prefix}-${random_id.environment.hex}"
  producer_profile = var.producer_profile != null ? var.producer_profile : var.profile
  consumer_profile = var.consumer_profile != null ? var.consumer_profile : var.profile
  cross_account    = data.aws_caller_identity.producer.account_id != data.aws_caller_identity.consumer.account_id
  sso_enabled      = var.identity_center_instance_arn != null

  # The producer's native fixture table and its Glue counterpart share one name.
  fixture_table_name = "fixture"
  # Consumer-local table and view used for annotations and object grants.
  local_table_name = "example_events"
  local_view_name  = "example_event_labels"
}
