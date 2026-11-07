data "redshift_external_schemas" "analytics" {
  database = "analytics"
}

output "glue_databases" {
  value = {
    for schema in data.redshift_external_schemas.analytics.external_schemas :
    schema.name => schema.glue_database if schema.source_type == "DATA_CATALOG"
  }
}
