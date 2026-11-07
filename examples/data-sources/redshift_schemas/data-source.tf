data "redshift_schemas" "analytics" {
  database = "analytics"
}

output "schema_quotas" {
  value = { for schema in data.redshift_schemas.analytics.schemas : schema.name => schema.quota }
}
