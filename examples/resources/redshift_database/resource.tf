resource "redshift_database" "analytics" {
  name          = "analytics"
  datashare_arn = aws_redshift_data_share_consumer_association.analytics.data_share_arn

  # Wait up to 15 minutes, instead of 5, for the associated share to appear in the SQL catalog.
  timeouts {
    create = "15m"
  }
}

resource "redshift_database" "local" {
  name             = "warehouse"
  owner            = "etl"
  connection_limit = 50
  # On an empty database, changing this runs ALTER in place; existing user tables make Redshift reject it safely.
  collation       = "CASE_INSENSITIVE"
  isolation_level = "SNAPSHOT"
}

# The read-only destination of an active zero-ETL integration. The warehouse must set enable_case_sensitive_identifier.
resource "redshift_database" "replica" {
  name = "orders_replica"

  integration {
    integration_id   = "a1b2c3d4-5678-90ab-cdef-example11111"
    source_database  = "orders"
    refresh_interval = 600
    query_all_states = true
  }
}

# DynamoDB uses aws_redshift_integration, not aws_rds_integration. Its source needs PITR and an export resource policy.
resource "redshift_database" "dynamodb_replica" {
  name = "dynamodb_replica"

  integration {
    integration_id   = "b2c3d4e5-6789-01ab-cdef-example22222"
    refresh_interval = 900
    # DynamoDB sources do not take source_database.
  }
}
