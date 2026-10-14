data "redshift_templates" "staging" {
  database = "analytics"
  schema   = "staging"
}

output "staging_templates" {
  value = [for template in data.redshift_templates.staging.templates : "${template.schema}.${template.name}"]
}
