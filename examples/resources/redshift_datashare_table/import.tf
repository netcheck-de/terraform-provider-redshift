import {
  to = redshift_datashare_table.view
  id = jsonencode({
    database  = "warehouse"
    datashare = "analytics"
    schema    = "serving"
    table     = "example_view"
  })
}
