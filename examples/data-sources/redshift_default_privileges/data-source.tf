# Default privileges of objects the loader creates in one database.
data "redshift_default_privileges" "loader" {
  database_name = "analytics"
  owner         = "loader"
}

output "loader_defaults" {
  value = [for rule in data.redshift_default_privileges.loader.default_privileges : "${coalesce(rule.schema_name, "*")} ${rule.object_type} -> ${rule.grantee}: ${join(", ", rule.privileges)}"]
}
