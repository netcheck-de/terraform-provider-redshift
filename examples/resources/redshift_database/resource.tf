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
  collation        = "CASE_INSENSITIVE"
  isolation_level  = "SNAPSHOT"
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
