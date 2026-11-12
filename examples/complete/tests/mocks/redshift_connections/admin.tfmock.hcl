# These provider aliases are dedicated to ordinary administration-database connection probes.
mock_data "redshift_database" {
  defaults = { database_type = "LOCAL" }
}
