override_data {
  target = data.redshift_database.zero_etl[0]
  values = {
    database_type = "LOCAL"
    integration = {
      integration_id   = "a1b2c3d4-5678-90ab-cdef-example11111"
      source_database  = null
      refresh_interval = 900
      auto_remediation = false
    }
  }
}
