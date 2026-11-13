import {
  to = redshift_datashare_grant.consumer
  id = jsonencode({
    database   = "warehouse"
    datashare  = "analytics"
    account_id = "123456789012"
  })
}
