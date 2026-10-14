# Optional zero-ETL integration from an existing Aurora or RDS source into the consumer warehouse. The source needs the
# engine settings AWS documents for zero-ETL, which this example does not manage.
variable "zero_etl_source_arn" {
  description = "Existing Aurora or RDS DB cluster or instance ARN to replicate into the consumer warehouse, which then also sets enable_case_sensitive_identifier; null skips the zero-ETL integration."
  type        = string
  default     = null

  validation {
    condition     = var.zero_etl_source_arn == null ? true : can(regex("^arn:[a-z0-9-]+:rds:[a-z0-9-]+:[0-9]{12}:(cluster|db):[a-zA-Z0-9-]+$", var.zero_etl_source_arn))
    error_message = "Supply an RDS DB cluster or instance ARN, or leave it null."
  }
}

variable "zero_etl_source_database" {
  description = "Source database to replicate; required for Aurora PostgreSQL, RDS for PostgreSQL, and RDS for Oracle sources, and null for MySQL sources."
  type        = string
  default     = null
}

locals {
  zero_etl_enabled = var.zero_etl_source_arn != null
}

# The target namespace authorizes the source and lets the account create integrations into it.
resource "aws_redshift_resource_policy" "zero_etl" {
  provider     = aws.consumer
  count        = local.zero_etl_enabled ? 1 : 0
  resource_arn = aws_redshiftserverless_namespace.consumer.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = "redshift:AuthorizeInboundIntegration"
        Resource  = aws_redshiftserverless_namespace.consumer.arn
        Condition = { StringEquals = { "aws:SourceArn" = var.zero_etl_source_arn } }
      },
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:${data.aws_partition.consumer.partition}:iam::${data.aws_caller_identity.consumer.account_id}:root" }
        Action    = "redshift:CreateInboundIntegration"
        Resource  = aws_redshiftserverless_namespace.consumer.arn
      },
    ]
  })
}

resource "aws_rds_integration" "zero_etl" {
  provider         = aws.consumer
  count            = local.zero_etl_enabled ? 1 : 0
  integration_name = "${local.name}-zero-etl"
  source_arn       = var.zero_etl_source_arn
  target_arn       = aws_redshiftserverless_namespace.consumer.arn

  lifecycle {
    precondition {
      condition = contains(
        [for parameter in aws_redshiftserverless_workgroup.consumer.config_parameter : "${parameter.parameter_key}=${parameter.parameter_value}"],
        "enable_case_sensitive_identifier=true",
      )
      error_message = "Zero-ETL targets require enable_case_sensitive_identifier on the consumer workgroup."
    }
  }

  depends_on = [aws_redshift_resource_policy.zero_etl]
}

locals {
  # SVV_INTEGRATION identifies the integration by the last segment of its ARN.
  zero_etl_integration_id = local.zero_etl_enabled ? element(reverse(split(":", aws_rds_integration.zero_etl[0].arn)), 0) : null
}

# The destination is read-only and replicates with the admin connection's permissions. Destroying it drops the
# replicated data; the integration itself is an AWS resource above.
resource "redshift_database" "zero_etl" {
  provider = redshift.consumer
  count    = local.zero_etl_enabled ? 1 : 0
  name     = "example_zero_etl"

  integration {
    integration_id   = local.zero_etl_integration_id
    source_database  = var.zero_etl_source_database
    refresh_interval = 600
    query_all_states = true
  }
}

data "redshift_database" "zero_etl" {
  provider = redshift.consumer
  count    = local.zero_etl_enabled ? 1 : 0
  name     = redshift_database.zero_etl[0].name
}
