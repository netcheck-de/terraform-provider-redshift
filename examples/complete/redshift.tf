resource "aws_redshift_subnet_group" "producer" {
  provider   = aws.producer
  name       = "${local.name}-producer"
  subnet_ids = local.producer_subnet_ids
}

resource "aws_redshift_parameter_group" "producer" {
  provider = aws.producer
  name     = "${local.name}-producer"
  family   = "redshift-1.0"

  parameter {
    name  = "require_ssl"
    value = "true"
  }
}

# Disposable encrypted cluster uses the AWS-managed Redshift key.
# trivy:ignore:AWS-0084
resource "aws_redshift_cluster" "producer" {
  provider                     = aws.producer
  cluster_identifier           = "${local.name}-producer"
  node_type                    = var.cluster_node_type
  database_name                = var.admin_database
  master_username              = var.admin_username
  manage_master_password       = true
  publicly_accessible          = var.allow_public_sql
  enhanced_vpc_routing         = true
  cluster_subnet_group_name    = aws_redshift_subnet_group.producer.name
  cluster_parameter_group_name = aws_redshift_parameter_group.producer.name
  vpc_security_group_ids       = [aws_security_group.producer.id]
  iam_roles                    = [aws_iam_role.producer.arn]
  default_iam_role_arn         = aws_iam_role.producer.arn
  skip_final_snapshot          = true

  depends_on = [
    aws_iam_role_policy.producer_fixture,
    aws_s3_object.fixture,
    module.fixture_bucket,
    module.producer_vpc,
    module.producer_endpoints,
    aws_vpc_security_group_egress_rule.producer_vpc,
    aws_vpc_security_group_egress_rule.producer_s3,
    aws_vpc_security_group_ingress_rule.producer_sql,
  ]
}

resource "aws_redshiftserverless_namespace" "consumer" {
  provider              = aws.consumer
  namespace_name        = "${local.name}-consumer"
  db_name               = var.admin_database
  admin_username        = var.admin_username
  manage_admin_password = true
  default_iam_role_arn  = aws_iam_role.consumer.arn
  # Reuse this stable role for optional SSO; toggling SSO must not change namespace attachment/provider credentials.
  iam_roles = [aws_iam_role.consumer.arn]

  depends_on = [aws_iam_role_policy.consumer_fixture, aws_iam_role_policy.identity_center]
}

resource "aws_redshiftserverless_workgroup" "consumer" {
  provider             = aws.consumer
  workgroup_name       = "${local.name}-consumer"
  namespace_name       = aws_redshiftserverless_namespace.consumer.namespace_name
  base_capacity        = var.serverless_capacity
  max_capacity         = var.serverless_capacity
  enhanced_vpc_routing = true
  publicly_accessible  = var.allow_public_sql
  subnet_ids           = local.consumer_subnet_ids
  security_group_ids   = [aws_security_group.consumer.id]

  # AWS provider 6.67 supports disabling scaling alongside explicit base capacity.
  price_performance_target {
    enabled = false
  }

  config_parameter {
    parameter_key   = "require_ssl"
    parameter_value = "true"
  }

  depends_on = [
    module.consumer_vpc,
    module.consumer_endpoints,
    aws_s3_object.fixture,
    module.fixture_bucket,
    aws_glue_resource_policy.fixture,
    aws_vpc_security_group_egress_rule.consumer_vpc,
    aws_vpc_security_group_egress_rule.consumer_s3,
    aws_vpc_security_group_ingress_rule.consumer_sql,
  ]
}
