import {
  to = redshift_function.greater
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "analytics"
    schema         = "reporting"
    name           = "f_sql_greater"
    arguments      = "double precision, double precision"
  })
}
