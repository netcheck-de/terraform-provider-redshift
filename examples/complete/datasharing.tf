locals {
  # The SQL datashare resource exposes no ARN; derive its AWS identity from the cluster namespace ARN.
  datashare_arn = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.consumer.datashare}"
}

resource "redshift_datashare" "producer" {
  provider            = redshift.producer
  database            = redshift_database.producer.name
  name                = "example_share"
  publicly_accessible = var.allow_public_sql
}

data "redshift_datashare" "producer" {
  provider = redshift.producer
  database = redshift_datashare.producer.database
  name     = redshift_datashare.producer.name
}

resource "redshift_datashare_schema" "source" {
  provider    = redshift.producer
  database    = redshift_datashare.producer.database
  datashare   = redshift_datashare.producer.name
  schema      = data.redshift_schema.source.name
  include_new = true

  # Seed first so future-object inclusion cannot race the explicitly managed table membership.
  depends_on = [aws_redshiftdata_statement.source_seed]
}

data "redshift_datashare_schema" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_schema.source.database
  datashare = redshift_datashare_schema.source.datashare
  schema    = redshift_datashare_schema.source.schema
}

resource "redshift_datashare_table" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_schema.source.database
  datashare = redshift_datashare_schema.source.datashare
  schema    = redshift_datashare_schema.source.schema
  table     = local.fixture_table_name

  depends_on = [aws_redshiftdata_statement.source_seed]
}

data "redshift_datashare_table" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_table.source.database
  datashare = redshift_datashare_table.source.datashare
  schema    = redshift_datashare_table.source.schema
  table     = redshift_datashare_table.source.table
}

# Same-account sharing is namespace-scoped; cross-account mode uses an account grant plus AWS authorization.
resource "redshift_datashare_grant" "consumer" {
  provider     = redshift.producer
  database     = redshift_datashare.producer.database
  datashare    = redshift_datashare.producer.name
  account_id   = local.cross_account ? data.aws_caller_identity.consumer.account_id : null
  namespace_id = local.cross_account ? null : aws_redshiftserverless_namespace.consumer.namespace_id

  depends_on = [redshift_datashare_table.source]
}

data "redshift_datashare_grant" "consumer" {
  provider     = redshift.producer
  database     = redshift_datashare_grant.consumer.database
  datashare    = redshift_datashare_grant.consumer.datashare
  account_id   = redshift_datashare_grant.consumer.account_id
  namespace_id = redshift_datashare_grant.consumer.namespace_id
}

resource "aws_redshift_data_share_authorization" "consumer" {
  provider            = aws.producer
  count               = local.cross_account ? 1 : 0
  consumer_identifier = data.aws_caller_identity.consumer.account_id
  data_share_arn      = local.datashare_arn
}

resource "aws_redshift_data_share_consumer_association" "this" {
  provider       = aws.consumer
  count          = local.cross_account ? 1 : 0
  data_share_arn = aws_redshift_data_share_authorization.consumer[0].data_share_arn
  consumer_arn   = aws_redshiftserverless_namespace.consumer.arn
  allow_writes   = false
}

resource "redshift_database" "shared" {
  provider      = redshift.consumer
  name          = "example_shared"
  datashare_arn = local.datashare_arn

  depends_on = [aws_redshift_data_share_consumer_association.this]
}

data "redshift_database" "shared" {
  provider = redshift.consumer
  name     = redshift_database.shared.name
}
