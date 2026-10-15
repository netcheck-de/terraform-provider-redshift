locals {
  # The SQL datashare resource exposes no ARN; derive its AWS identity from the cluster namespace ARN.
  datashare_arn = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.consumer.datashare}"
}

resource "redshift_datashare" "producer" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "example_share"
  # A publicly accessible consumer workgroup can only consume shares that allow public access.
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

  # A fresh association can take a while to reach the SQL catalog.
  timeouts {
    create = "15m"
  }

  depends_on = [aws_redshift_data_share_consumer_association.this]
}

data "redshift_database" "shared" {
  provider = redshift.consumer
  name     = redshift_database.shared.name
}

# A second share receives schema permissions through scoped grants instead of explicit membership resources.
# Grants and membership resources must not manage the same share/schema tuple, so this share stays separate.
resource "redshift_datashare" "grants" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "example_share_grants"
}

resource "redshift_grant" "share_schema" {
  for_each = {
    SCHEMA = ["USAGE"]
    TABLES = ["SELECT"]
  }
  provider      = redshift.producer
  database_name = redshift_datashare.grants.database
  schema_name   = data.redshift_schema.source.name
  datashare     = redshift_datashare.grants.name
  scope         = each.key
  privileges    = each.value

  depends_on = [aws_redshiftdata_statement.source_table]
}

data "redshift_grant" "share_schema" {
  provider      = redshift.producer
  database_name = redshift_grant.share_schema["TABLES"].database_name
  schema_name   = redshift_grant.share_schema["TABLES"].schema_name
  datashare     = redshift_grant.share_schema["TABLES"].datashare
  scope         = redshift_grant.share_schema["TABLES"].scope
}

# A producer-side role administers the grants share without owning it: ALTER changes its objects, SHARE its consumers.
resource "redshift_role" "share_operators" {
  provider = redshift.producer
  name     = "example_share_operators"
}

resource "redshift_datashare_privilege" "share_operators" {
  provider       = redshift.producer
  database_name  = redshift_datashare.grants.database
  datashare_name = redshift_datashare.grants.name
  grantee_type   = "ROLE"
  grantee        = redshift_role.share_operators.name
  privileges     = ["ALTER", "SHARE"]
}

data "redshift_datashare_privilege" "share_operators" {
  provider       = redshift.producer
  database_name  = redshift_datashare_privilege.share_operators.database_name
  datashare_name = redshift_datashare_privilege.share_operators.datashare_name
  grantee_type   = redshift_datashare_privilege.share_operators.grantee_type
  grantee        = redshift_datashare_privilege.share_operators.grantee
}

# Lake Formation consumers receive usage VIA DATA CATALOG and an AWS authorization for the DataCatalog/ identifier.
resource "redshift_datashare_grant" "lake_formation" {
  provider         = redshift.producer
  count            = var.lake_formation_account_id == null ? 0 : 1
  database         = redshift_datashare.grants.database
  datashare        = redshift_datashare.grants.name
  account_id       = var.lake_formation_account_id
  via_data_catalog = true

  depends_on = [redshift_grant.share_schema]
}

resource "aws_redshift_data_share_authorization" "lake_formation" {
  provider            = aws.producer
  count               = var.lake_formation_account_id == null ? 0 : 1
  data_share_arn      = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.lake_formation[0].datashare}"
  consumer_identifier = "DataCatalog/${redshift_datashare_grant.lake_formation[0].account_id}"
}

# Listings observe both sides of the share: the producer's outbound shares and the consumer's inbound share.
data "redshift_datashares" "outbound" {
  provider   = redshift.producer
  share_type = "OUTBOUND"

  depends_on = [redshift_datashare.producer, redshift_datashare.grants]
}

data "redshift_datashares" "inbound" {
  provider   = redshift.consumer
  share_type = "INBOUND"
  name       = redshift_datashare.producer.name

  depends_on = [redshift_database.shared]
}
