import {
  to = redshift_function.greater
  id = jsonencode({
    database  = "analytics"
    schema    = "reporting"
    name      = "f_sql_greater"
    arguments = "DOUBLE PRECISION, DOUBLE PRECISION"
  })
}
