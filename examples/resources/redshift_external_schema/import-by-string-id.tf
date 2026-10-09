import {
  to = redshift_external_schema.raw
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    name           = "raw"
  })
}
