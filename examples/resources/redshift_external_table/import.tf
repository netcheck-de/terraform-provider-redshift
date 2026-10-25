import {
  to = redshift_external_table.sales
  id = jsonencode({
    database = "warehouse"
    schema   = "raw"
    name     = "sales"
  })
}
