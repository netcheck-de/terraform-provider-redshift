import {
  to = redshift_datashare_table.view
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    datashare      = "analytics"
    schema         = "serving"
    table          = "example_view"
  })
}
