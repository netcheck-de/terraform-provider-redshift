variable "zero_etl_create_dynamodb_source" {
  description = "Create a disposable on-demand DynamoDB table with PITR and an export resource policy as the zero-ETL source. Adds DynamoDB, backup, and export charges. Mutually exclusive with zero_etl_create_source and zero_etl_source_arn."
  type        = bool
  default     = false

  validation {
    condition     = !var.zero_etl_create_dynamodb_source || var.zero_etl_source_arn == null
    error_message = "Set either zero_etl_create_dynamodb_source or zero_etl_source_arn, not both."
  }
}

# The default AWS-owned encryption is supported; DynamoDB's AWS-managed KMS key is not supported for zero-ETL.
resource "aws_dynamodb_table" "zero_etl_source" {
  provider     = aws.consumer
  count        = var.zero_etl_create_dynamodb_source ? 1 : 0
  name         = "${local.name}-zero-etl-source"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }
}

# Scope the exporter to integrations in the consumer account and Region, without depending on the integration ARN
# (the policy must exist before creating it). Existing tables keep their operator-managed resource policy.
resource "aws_dynamodb_resource_policy" "zero_etl_source" {
  provider     = aws.consumer
  count        = var.zero_etl_create_dynamodb_source ? 1 : 0
  resource_arn = aws_dynamodb_table.zero_etl_source[0].arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = ["dynamodb:DescribeTable", "dynamodb:ExportTableToPointInTime"]
        Resource  = aws_dynamodb_table.zero_etl_source[0].arn
        Condition = {
          StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.consumer.account_id }
          ArnLike      = { "aws:SourceArn" = "arn:${data.aws_partition.consumer.partition}:redshift:${var.region}:${data.aws_caller_identity.consumer.account_id}:integration:*" }
        }
      },
      {
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = "dynamodb:DescribeExport"
        Resource  = "${aws_dynamodb_table.zero_etl_source[0].arn}/export/*"
        Condition = {
          StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.consumer.account_id }
          ArnLike      = { "aws:SourceArn" = "arn:${data.aws_partition.consumer.partition}:redshift:${var.region}:${data.aws_caller_identity.consumer.account_id}:integration:*" }
        }
      },
    ]
  })
}

# DynamoDB integrations belong to the Redshift API, not the RDS API. The shared namespace policy already authorizes
# local.zero_etl_source_arn, and local.zero_etl_enabled also turns on case-sensitive identifiers on the workgroup.
resource "aws_redshift_integration" "zero_etl" {
  provider         = aws.consumer
  count            = local.zero_etl_enabled && local.zero_etl_dynamodb ? 1 : 0
  integration_name = "${local.name}-zero-etl"
  source_arn       = local.zero_etl_source_arn
  target_arn       = aws_redshiftserverless_namespace.consumer.arn

  depends_on = [
    aws_dynamodb_resource_policy.zero_etl_source,
    aws_redshift_resource_policy.inbound_integrations,
    aws_redshiftserverless_workgroup.consumer,
  ]
}
