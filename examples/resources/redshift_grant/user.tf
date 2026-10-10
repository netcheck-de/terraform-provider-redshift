resource "redshift_grant" "analyst_tables" {
  database_name           = "analytics"
  schema_name             = "serving"
  user                    = redshift_user.analyst.name
  scope                   = "TABLES"
  privileges              = ["SELECT", "INSERT"]
  grant_option_privileges = ["SELECT"]
}

resource "redshift_grant" "developer_languages" {
  database_name = "analytics"
  role          = redshift_role.developers.name
  scope         = "LANGUAGES"
  privileges    = ["USAGE"]
}

resource "redshift_grant" "loader_copy_jobs" {
  database_name = "analytics"
  user          = redshift_user.loader.name
  scope         = "COPY JOBS"
  privileges    = ["CREATE", "ALTER", "DROP"]
}

resource "redshift_grant" "reader_templates" {
  database_name = "analytics"
  schema_name   = "serving"
  role          = redshift_role.readers.name
  scope         = "TEMPLATES"
  privileges    = ["USAGE"]
}
