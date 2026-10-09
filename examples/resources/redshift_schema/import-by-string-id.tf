import {
  to = redshift_schema.serving
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    name           = "serving"
  })
}
