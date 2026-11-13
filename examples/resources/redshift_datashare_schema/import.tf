import {
  to = redshift_datashare_schema.serving
  id = jsonencode({
    database  = "warehouse"
    datashare = "analytics"
    schema    = "serving"
  })
}
