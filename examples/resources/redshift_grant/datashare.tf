resource "redshift_grant" "share_schema" {
  database_name = "analytics"
  schema_name   = "serving"
  datashare     = "analytics_share"
  scope         = "SCHEMA"
  privileges    = ["USAGE"]
}

resource "redshift_grant" "share_tables" {
  database_name = "analytics"
  schema_name   = "serving"
  datashare     = "analytics_share"
  scope         = "TABLES"
  privileges    = ["SELECT"]
  depends_on    = [redshift_grant.share_schema]
}
