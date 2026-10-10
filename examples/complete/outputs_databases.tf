output "database_options" {
  description = "Owner and options read back for the producer database and the consumer-local schema."
  value = {
    producer_owner     = data.redshift_database.producer.owner
    producer_collation = data.redshift_database.producer.collation
    producer_isolation = data.redshift_database.producer.isolation_level
    producer_limit     = data.redshift_database.producer.connection_limit
    local_schema_quota = data.redshift_schema.local.quota
  }
}

output "shared_external_schema" {
  description = "Cross-database external schema exposing the consumer's datashare database in the local database."
  value = {
    id              = data.redshift_external_schema.shared.id
    source_type     = data.redshift_external_schema.shared.source_type
    source_database = data.redshift_external_schema.shared.source_database
    source_schema   = data.redshift_external_schema.shared.source_schema
    owner           = data.redshift_external_schema.shared.owner
  }
}
