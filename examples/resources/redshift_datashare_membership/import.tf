import {
  to = redshift_datashare_membership.functions
  id = jsonencode({
    database  = "analytics"
    datashare = "reporting"
    schema    = "reporting"
    kind      = "FUNCTIONS"
  })
}
