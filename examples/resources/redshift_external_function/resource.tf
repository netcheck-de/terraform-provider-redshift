resource "redshift_external_function" "upper" {
  database        = redshift_database.warehouse.name
  schema          = "public"
  name            = "f_upper"
  arguments       = ["varchar"]
  return_type     = "varchar"
  volatility      = "STABLE"
  lambda_function = aws_lambda_function.upper.function_name
  iam_role        = aws_iam_role.lambda_invoker.arn
  retry_timeout   = 3000
  max_batch_rows  = 500
}
