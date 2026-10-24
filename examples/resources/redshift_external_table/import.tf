import {
  to = redshift_external_table.sales
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    schema         = "raw"
    name           = "sales"
  })
}
