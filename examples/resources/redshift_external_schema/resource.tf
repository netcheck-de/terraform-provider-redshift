resource "redshift_external_schema" "raw" {
  database      = redshift_database.warehouse.name
  name          = "raw"
  glue_database = "raw_catalog"
  iam_role_arn  = aws_iam_role.spectrum.arn
  owner         = "etl"
}

resource "redshift_external_schema" "orders" {
  database        = redshift_database.warehouse.name
  name            = "orders"
  source_type     = "POSTGRES"
  source_database = "orders"
  source_schema   = "public"
  uri             = aws_rds_cluster.orders.endpoint
  port            = 5432
  iam_role_arn    = aws_iam_role.federated.arn
  secret_arn      = aws_secretsmanager_secret.orders.arn
}

resource "redshift_external_schema" "shared_sales" {
  database        = redshift_database.warehouse.name
  name            = "shared_sales"
  source_type     = "REDSHIFT"
  source_database = redshift_database.analytics.name
  source_schema   = "public"
}

resource "redshift_external_schema" "clicks" {
  database       = redshift_database.warehouse.name
  name           = "clicks"
  source_type    = "MSK"
  authentication = "IAM"
  iam_role_arn   = aws_iam_role.streaming.arn
  uri            = aws_msk_cluster.clicks.bootstrap_brokers_sasl_iam
}
