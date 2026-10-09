data "redshift_datashare_grant" "consumer" {
  database   = "analytics"
  datashare  = "reports"
  account_id = "123456789012"
}

data "redshift_datashare_grant" "namespace" {
  database     = "analytics"
  datashare    = "reports"
  namespace_id = "12345678-1234-1234-1234-123456789abc"
}
