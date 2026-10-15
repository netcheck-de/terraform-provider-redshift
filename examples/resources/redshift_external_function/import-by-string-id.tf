import {
  to = redshift_external_function.upper
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "warehouse"
    schema         = "public"
    name           = "f_upper"
    arguments      = "character varying"
  })
}
