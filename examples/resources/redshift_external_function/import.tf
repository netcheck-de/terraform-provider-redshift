import {
  to = redshift_external_function.upper
  id = jsonencode({
    database  = "warehouse"
    schema    = "public"
    name      = "f_upper"
    arguments = "CHARACTER VARYING"
  })
}
