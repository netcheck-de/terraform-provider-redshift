import {
  to = redshift_grant.readers["TABLES"]
  id = jsonencode({
    database_name = "analytics"
    role          = "ncidc:analytics-readers"
    scope         = "TABLES"
  })
}
