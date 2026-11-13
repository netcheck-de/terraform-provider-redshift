resource "aws_redshift_parameter_group" "this" {
  name   = "${var.name_prefix}-producer"
  family = "redshift-1.0"

  parameter {
    name  = "require_ssl"
    value = "true"
  }
}

# Disposable, encrypted single-node producer, independent of examples/complete.
resource "aws_redshift_cluster" "producer" {
  cluster_identifier           = "${var.name_prefix}-producer"
  node_type                    = "ra3.large"
  cluster_type                 = "single-node"
  database_name                = var.db_name
  master_username              = "tfadmin"
  manage_master_password       = true
  encrypted                    = true
  publicly_accessible          = true
  enhanced_vpc_routing         = true
  cluster_subnet_group_name    = module.vpc.redshift_subnet_group
  cluster_parameter_group_name = aws_redshift_parameter_group.this.name
  vpc_security_group_ids       = [aws_security_group.this.id]
  iam_roles                    = [aws_iam_role.this.arn]
  default_iam_role_arn         = aws_iam_role.this.arn
  skip_final_snapshot          = true
}

resource "aws_redshiftserverless_namespace" "this" {
  namespace_name        = var.name_prefix
  db_name               = var.db_name
  admin_username        = "tfadmin"
  manage_admin_password = true
  default_iam_role_arn  = aws_iam_role.this.arn
  iam_roles             = [aws_iam_role.this.arn, aws_iam_role.lambda_invocation.arn]
}

resource "aws_redshiftserverless_workgroup" "this" {
  workgroup_name      = var.name_prefix
  namespace_name      = aws_redshiftserverless_namespace.this.namespace_name
  base_capacity       = 4
  max_capacity        = 4
  publicly_accessible = true
  subnet_ids          = module.vpc.redshift_subnets
  security_group_ids  = [aws_security_group.this.id]

  config_parameter {
    parameter_key   = "require_ssl"
    parameter_value = "true"
  }

  config_parameter {
    parameter_key   = "enable_case_sensitive_identifier"
    parameter_value = "true"
  }
}
