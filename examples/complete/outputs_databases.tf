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

output "database_collation_migration" {
  description = "Identity and fresh lookup collation of the deliberately empty migration-probe database."
  value = {
    id        = data.redshift_database.collation_probe.id
    name      = data.redshift_database.collation_probe.name
    collation = data.redshift_database.collation_probe.collation
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

output "database_listings" {
  description = "External schemas and comments listed in the consumer-local database."
  value = {
    external_schemas = { for schema in data.redshift_external_schemas.local.external_schemas : schema.name => schema.source_type }
    comments = [
      for comment in data.redshift_comments.local.comments :
      { object_type = comment.object_type, object_name = comment.object_name, text = comment.text }
    ]
  }
}
