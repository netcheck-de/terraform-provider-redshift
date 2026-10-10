data "redshift_databases" "analytics" {
  database_type = "local"
  name_like     = "analytics_%"
}

output "analytics_databases" {
  value = [for database in data.redshift_databases.analytics.databases : database.name]
}
