resource "redshift_function" "greater" {
  database    = "analytics"
  schema      = "reporting"
  name        = "f_sql_greater"
  arguments   = ["float", "float"]
  return_type = "float"
  volatility  = "STABLE"
  body        = <<-SQL
    SELECT CASE WHEN $1 > $2 THEN $1 ELSE $2 END
  SQL
}
