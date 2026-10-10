data "redshift_schemas" "external" {
  database    = "analytics"
  schema_type = "external"
}

output "external_schema_sources" {
  value = { for schema in data.redshift_schemas.external.items : schema.name => schema.source_database }
}
