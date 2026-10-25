import {
  to = redshift_external_partition.sales_2008_01
  id = jsonencode({
    database = "warehouse"
    schema   = "raw"
    table    = "sales"
    values   = jsonencode({ sale_date = "2008-01-01" })
  })
}
