# Optional zero-ETL integration into the consumer warehouse, either from an existing Aurora or RDS source, which needs
# the engine settings AWS documents for zero-ETL, or from a disposable Aurora PostgreSQL source the example creates.
variable "zero_etl_source_arn" {
  description = "Existing Aurora or RDS DB cluster or instance ARN to replicate into the consumer warehouse, which then also sets enable_case_sensitive_identifier; null skips the integration unless zero_etl_create_source is set."
  type        = string
  default     = null

  validation {
    condition     = var.zero_etl_source_arn == null ? true : can(regex("^arn:[a-z0-9-]+:rds:[a-z0-9-]+:[0-9]{12}:(cluster|db):[a-zA-Z0-9-]+$", var.zero_etl_source_arn))
    error_message = "Supply an RDS DB cluster or instance ARN, or leave it null."
  }
}

variable "zero_etl_source_database" {
  description = "Source database to replicate; required for Aurora PostgreSQL, RDS for PostgreSQL, and RDS for Oracle sources, and null for MySQL sources. Ignored with zero_etl_create_source."
  type        = string
  default     = null
}

variable "zero_etl_create_source" {
  description = "Create a disposable Aurora PostgreSQL Serverless v2 cluster with the zero-ETL parameters as the integration source, instead of zero_etl_source_arn. It adds about 20 minutes to apply and Aurora charges while it exists."
  type        = bool
  default     = false

  validation {
    condition     = !(var.zero_etl_create_source && var.zero_etl_source_arn != null)
    error_message = "Set either zero_etl_create_source or zero_etl_source_arn, not both."
  }
}

locals {
  zero_etl_enabled         = var.zero_etl_create_source || var.zero_etl_source_arn != null
  zero_etl_source_arn      = var.zero_etl_create_source ? aws_rds_cluster.zero_etl_source[0].arn : var.zero_etl_source_arn
  zero_etl_source_database = var.zero_etl_create_source ? aws_rds_cluster.zero_etl_source[0].database_name : var.zero_etl_source_database
}

# Aurora PostgreSQL replicates through enhanced logical replication; these static parameters must be in place when the
# writer starts, which a parameter group attached at creation guarantees.
resource "aws_rds_cluster_parameter_group" "zero_etl_source" {
  provider = aws.consumer
  count    = var.zero_etl_create_source ? 1 : 0
  name     = "${local.name}-zero-etl-source"
  family   = "aurora-postgresql16"

  dynamic "parameter" {
    for_each = {
      "rds.logical_replication"             = "1"
      "aurora.enhanced_logical_replication" = "1"
      "aurora.logical_replication_backup"   = "0"
      "aurora.logical_replication_globaldb" = "0"
    }

    content {
      name         = parameter.key
      value        = parameter.value
      apply_method = "pending-reboot"
    }
  }
}

resource "aws_db_subnet_group" "zero_etl_source" {
  provider   = aws.consumer
  count      = var.zero_etl_create_source ? 1 : 0
  name       = "${local.name}-zero-etl-source"
  subnet_ids = local.consumer_subnet_ids
}

data "aws_rds_engine_version" "zero_etl_source" {
  provider = aws.consumer
  count    = var.zero_etl_create_source ? 1 : 0
  engine   = "aurora-postgresql"
  version  = "16"
  latest   = true
}

# Zero-ETL replicates through AWS, so the source needs no network path to the warehouse. RDS keeps the master password
# in Secrets Manager; the example never connects to the source.
resource "aws_rds_cluster" "zero_etl_source" {
  provider                        = aws.consumer
  count                           = var.zero_etl_create_source ? 1 : 0
  cluster_identifier              = "${local.name}-zero-etl-source"
  engine                          = "aurora-postgresql"
  engine_version                  = data.aws_rds_engine_version.zero_etl_source[0].version_actual
  database_name                   = "orders"
  master_username                 = "orders_admin"
  manage_master_user_password     = true
  db_cluster_parameter_group_name = aws_rds_cluster_parameter_group.zero_etl_source[0].name
  db_subnet_group_name            = aws_db_subnet_group.zero_etl_source[0].name
  vpc_security_group_ids          = [aws_security_group.consumer.id]
  storage_encrypted               = true
  skip_final_snapshot             = true

  serverlessv2_scaling_configuration {
    min_capacity = 0.5
    max_capacity = 1
  }
}

resource "aws_rds_cluster_instance" "zero_etl_source" {
  provider           = aws.consumer
  count              = var.zero_etl_create_source ? 1 : 0
  identifier         = "${local.name}-zero-etl-source"
  cluster_identifier = aws_rds_cluster.zero_etl_source[0].id
  engine             = aws_rds_cluster.zero_etl_source[0].engine
  engine_version     = aws_rds_cluster.zero_etl_source[0].engine_version
  instance_class     = "db.serverless"
}

resource "aws_rds_integration" "zero_etl" {
  provider         = aws.consumer
  count            = local.zero_etl_enabled ? 1 : 0
  integration_name = "${local.name}-zero-etl"
  source_arn       = local.zero_etl_source_arn
  target_arn       = aws_redshiftserverless_namespace.consumer.arn
  # PostgreSQL and Oracle sources replicate only the databases a filter includes; MySQL sources default to everything.
  data_filter = local.zero_etl_source_database == null ? null : "include: ${local.zero_etl_source_database}.*.*"

  lifecycle {
    precondition {
      condition = contains(
        [for parameter in aws_redshiftserverless_workgroup.consumer.config_parameter : "${parameter.parameter_key}=${parameter.parameter_value}"],
        "enable_case_sensitive_identifier=true",
      )
      error_message = "Zero-ETL targets require enable_case_sensitive_identifier on the consumer workgroup."
    }
  }

  # A created source can replicate only once its writer instance runs.
  depends_on = [aws_redshift_resource_policy.inbound_integrations, aws_rds_cluster_instance.zero_etl_source]
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
    source_database  = local.zero_etl_source_database
    refresh_interval = 600
    query_all_states = true
  }
}

data "redshift_database" "zero_etl" {
  provider = redshift.consumer
  count    = local.zero_etl_enabled ? 1 : 0
  name     = redshift_database.zero_etl[0].name
}
