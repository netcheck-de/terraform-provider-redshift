import {
  to = redshift_datashare_schema.serving
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    datashare      = "analytics"
    schema         = "serving"
  })
}
