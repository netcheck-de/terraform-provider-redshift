output "zero_etl_database" {
  description = "Zero-ETL destination database and its integration read back from SVV_INTEGRATION, plus the integration ARN to delete before destroy; null without a zero-ETL source."
  value = local.zero_etl_enabled ? {
    name             = data.redshift_database.zero_etl[0].name
    database_type    = data.redshift_database.zero_etl[0].database_type
    owner            = data.redshift_database.zero_etl[0].owner
    integration_id   = data.redshift_database.zero_etl[0].integration.integration_id
    source_database  = data.redshift_database.zero_etl[0].integration.source_database
    refresh_interval = data.redshift_database.zero_etl[0].integration.refresh_interval
    history_mode     = data.redshift_database.zero_etl[0].integration.history_mode
    integration_arn  = local.zero_etl_integration_arn
  } : null
}
