data "redshift_function" "greater" {
  database  = "analytics"
  schema    = "reporting"
  name      = "f_sql_greater"
  arguments = ["FLOAT", "FLOAT"]
}
